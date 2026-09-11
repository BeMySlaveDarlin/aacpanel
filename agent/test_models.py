#!/usr/bin/env python3
import json
import os
import shutil
import sys
import unittest
import urllib.error

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import archive  # noqa: E402
import contours  # noqa: E402
import models  # noqa: E402

TOKEN = "sk-ant-oat01-secret-subscription-token"

ANSWER = {
    "data": [
        {"id": "claude-opus-5", "display_name": "Claude Opus 5",
         "max_input_tokens": 1_000_000, "max_tokens": 128_000, "type": "model"},
        {"id": "claude-haiku-4-5-20251001", "display_name": "Claude Haiku 4.5",
         "max_input_tokens": 200_000, "max_tokens": 64_000, "type": "model"},
    ],
    "has_more": False,
}


class Fake:
    def __init__(self, payload=None, error=None):
        self.payload = payload
        self.error = error
        self.requests = []

    def __call__(self, request, timeout=None):
        self.requests.append(request)
        if self.error is not None:
            raise self.error
        return self

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False

    def read(self, limit=None):
        return json.dumps(self.payload).encode("utf-8")


class Base(unittest.TestCase):
    def setUp(self):
        self.root = test_barrier.tmp_path(prefix="models-")
        self.addCleanup(shutil.rmtree, self.root, True)

        self.home = os.path.join(self.root, ".claude")
        os.makedirs(self.home)
        self.reg = os.path.join(self.root, "registry.conf")

        for mod, name in ((contours, "HOME"), (contours, "REGISTRY"),
                          (models, "CACHE_PATH")):
            self.addCleanup(setattr, mod, name, getattr(mod, name))
        contours.HOME = self.home
        contours.REGISTRY = self.reg
        models.CACHE_PATH = os.path.join(self.root, "models.json")
        self.addCleanup(setattr, models, "_windows_key", None)
        self.addCleanup(setattr, models, "_windows", {})
        models._windows_key, models._windows = None, {}

    def credentials(self, config_dir, token=TOKEN):
        os.makedirs(config_dir, exist_ok=True)
        with open(os.path.join(config_dir, ".credentials.json"), "w", encoding="utf-8") as f:
            json.dump({"claudeAiOauth": {"accessToken": token}}, f)

    def contour(self, name="work"):
        d = os.path.join(self.root, name)
        os.makedirs(d, exist_ok=True)
        with open(self.reg, "a", encoding="utf-8") as f:
            f.write(f"{name} | /opt/{name} | {d} | {d}/token\n")
        return d

    def patch(self, fake):
        old = models.urllib.request.urlopen
        self.addCleanup(setattr, models.urllib.request, "urlopen", old)
        models.urllib.request.urlopen = fake
        return fake


class Catalog(Base):
    def test_it_collects_the_models_of_a_contour(self):
        self.credentials(self.home)
        self.patch(Fake(ANSWER))

        got = models.catalog()
        rows = got["contours"]
        self.assertEqual([c["profile"] for c in rows], ["personal"])
        self.assertEqual(rows[0]["state"], "ok")
        self.assertEqual(rows[0]["models"][0],
                         {"id": "claude-opus-5", "name": "Claude Opus 5",
                          "window": 1_000_000, "output": 128_000})

    def test_the_order_from_the_endpoint_is_kept(self):
        self.credentials(self.home)
        self.patch(Fake(ANSWER))
        got = models.catalog()
        self.assertEqual([m["id"] for m in got["contours"][0]["models"]],
                         ["claude-opus-5", "claude-haiku-4-5-20251001"])

    def test_the_data_stays_per_contour(self):
        self.credentials(self.home)
        self.contour()
        self.patch(Fake(ANSWER))

        rows = {c["profile"]: c for c in models.catalog()["contours"]}
        self.assertEqual(rows["personal"]["state"], "ok")
        self.assertEqual(rows["work"]["state"], "unknown")
        self.assertNotIn("models", rows["work"])

    def test_a_network_failure_keeps_the_previous_list(self):
        self.credentials(self.home)
        self.patch(Fake(ANSWER))
        models.catalog(now=1000)

        self.patch(Fake(error=urllib.error.URLError("connection refused")))
        row = models.catalog(now=1000 + models.REFRESH + 1)["contours"][0]
        self.assertEqual(row["state"], "ok", "the previous catalog was thrown away because of the network")
        self.assertEqual(row["error"], "network")
        self.assertEqual(len(row["models"]), 2)

    def test_it_does_not_go_to_the_network_more_often_than_once_a_day(self):
        self.credentials(self.home)
        fake = self.patch(Fake(ANSWER))
        models.catalog(now=1000)
        models.catalog(now=1000 + models.REFRESH - 1)
        self.assertEqual(len(fake.requests), 1, "the request was repeated before its time")
        models.catalog(now=1000 + models.REFRESH + 1)
        self.assertEqual(len(fake.requests), 2, "the request was not repeated when its time came")

    def test_an_empty_cache_path_cancels_the_request_too(self):
        self.credentials(self.home)
        for empty in ("", None):
            with self.subTest(path=empty):
                models.CACHE_PATH = empty
                fake = self.patch(Fake(ANSWER))
                self.assertIsNone(models.catalog(), "the catalog was built with nowhere to cache it")
                self.assertEqual(fake.requests, [], "a request went out with the catalog switched off")


class Secret(Base):
    def leaked(self, where, text):
        self.assertNotIn(TOKEN, text, f"the subscription token leaked into {where}")

    def test_a_successful_request(self):
        self.credentials(self.home)
        fake = self.patch(Fake(ANSWER))

        got = models.catalog()

        headers = {k.lower(): v for k, v in fake.requests[0].header_items()}
        self.assertEqual(headers["authorization"], f"Bearer {TOKEN}")

        self.leaked("the snapshot", json.dumps(got, ensure_ascii=False))
        with open(models.CACHE_PATH, encoding="utf-8") as f:
            self.leaked("the cache on disk", f.read())

    def test_a_failure_with_the_token_in_the_error_text(self):
        self.credentials(self.home)
        error = urllib.error.HTTPError(
            models.ENDPOINT, 401, f"Unauthorized: Bearer {TOKEN}", {}, None)
        self.patch(Fake(error=error))

        got = models.catalog()
        row = got["contours"][0]
        self.assertEqual(row["state"], "error")
        self.assertEqual(row["error"], "http-401", "the error text went out instead of the code")

        self.leaked("the snapshot", json.dumps(got, ensure_ascii=False))
        with open(models.CACHE_PATH, encoding="utf-8") as f:
            self.leaked("the cache on disk", f.read())


class Windows(Base):
    def test_the_window_comes_from_the_catalog(self):
        self.credentials(self.home)
        self.patch(Fake(ANSWER))
        models.catalog()

        self.assertEqual(models.window_for("claude-haiku-4-5-20251001"), 200_000)
        self.assertEqual(models.window_for("claude-opus-5"), 1_000_000)
        self.assertIsNone(models.window_for("claude-no-such-2099"))

    def test_the_window_suffix_is_stripped(self):
        self.credentials(self.home)
        self.patch(Fake(ANSWER))
        models.catalog()
        self.assertEqual(models.window_for("claude-opus-5[1m]"), 1_000_000)

    def test_haiku_no_longer_counts_as_a_million_window(self):
        self.credentials(self.home)
        self.patch(Fake(ANSWER))
        models.catalog()

        limit, known = archive.limit_for("claude-haiku-4-5-20251001")
        self.assertEqual(limit, 200_000)
        self.assertTrue(known)

    def test_without_a_catalog_the_fallback_map_remains(self):
        models.CACHE_PATH = None
        models._windows_key, models._windows = None, {}
        self.assertEqual(archive.limit_for("claude-opus-5"), (1_000_000, True))
        self.assertEqual(archive.limit_for("something-of-our-own"),
                         (archive.DEFAULT_LIMIT_TOKENS, False))


if __name__ == "__main__":
    unittest.main()
