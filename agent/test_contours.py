import importlib
import json
import os
import shutil
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import agent  # noqa: E402
import archive  # noqa: E402
import chat  # noqa: E402
import contours  # noqa: E402
import sesstate  # noqa: E402

UUID_A = "11111111-1111-1111-1111-111111111111"
UUID_B = "22222222-2222-2222-2222-222222222222"


class Contours(unittest.TestCase):
    def setUp(self):
        self.root = test_barrier.tmp_path(prefix="contours-")
        self.addCleanup(shutil.rmtree, self.root, True)
        self.home = os.path.join(self.root, ".claude")
        for name in ("sessions", "projects", "teams"):
            os.makedirs(os.path.join(self.home, name))
        self.reg = os.path.join(self.root, "registry.conf")

        for mod, name in ((contours, "HOME"), (contours, "REGISTRY"),
                          (agent, "CLAUDE_SESSIONS"), (agent, "LIMITS_PATH"),
                          (chat, "PROJECTS_DIR"), (archive, "PROJECTS"),
                          (archive, "LIVE"), (sesstate, "TEAMS_DIR")):
            self.addCleanup(setattr, mod, name, getattr(mod, name))
        contours.HOME = self.home
        contours.REGISTRY = self.reg
        for mod, name in ((agent, "CLAUDE_SESSIONS"), (agent, "LIMITS_PATH"),
                          (chat, "PROJECTS_DIR"), (archive, "PROJECTS"),
                          (archive, "LIVE"), (sesstate, "TEAMS_DIR")):
            setattr(mod, name, None)

    def contour(self, name="work"):
        d = os.path.join(self.root, name)
        for part in ("sessions", "projects", "teams"):
            os.makedirs(os.path.join(d, part))
        with open(self.reg, "w", encoding="utf-8") as f:
            f.write("# profile | prefix | config | token\n"
                    "%s | /srv/proj/Labs/ | %s | ~/.vault/%s.age\n"
                    "personal | * | %s | -\n" % (name, d, name, self.home))
        return d

    def test_directories_from_the_environment_come_first(self):
        work = self.contour("work")
        acme = os.path.join(self.root, "acme")
        os.makedirs(os.path.join(acme, "sessions"))
        os.environ["AACP_CLAUDE_HOME"] = os.pathsep.join([self.home, acme])
        self.addCleanup(os.environ.pop, "AACP_CLAUDE_HOME", None)

        self.assertEqual(contours.config_dirs(), [self.home, acme, work])

    def test_without_the_environment_the_registry_remains(self):
        os.environ.pop("AACP_CLAUDE_HOME", None)
        work = self.contour("work")
        self.assertEqual(contours.config_dirs(), [self.home, work])

    def test_a_contour_name_from_the_environment_is_derived_from_its_path(self):
        acme = os.path.join(self.root, "acme")
        os.makedirs(acme)
        os.environ["AACP_CLAUDE_HOME"] = os.pathsep.join([self.home, acme])
        self.addCleanup(os.environ.pop, "AACP_CLAUDE_HOME", None)

        self.assertEqual(contours.profiles(),
                         [("personal", self.home), ("acme", acme)])

    def test_without_a_registry_only_the_personal_contour_remains(self):
        self.assertEqual(contours.config_dirs(), [self.home])
        self.assertEqual(agent.sessions_dirs(),
                         [os.path.join(self.home, "sessions")])

    def test_the_registry_is_read_only_from_the_environment(self):
        env = dict(os.environ)
        self.addCleanup(lambda: (os.environ.clear(), os.environ.update(env),
                                 importlib.reload(contours)))
        os.environ.pop("AACP_CLAUDE_REGISTRY", None)
        self.assertEqual(importlib.reload(contours).REGISTRY, "",
                         "the registry path did not come from the environment")

        os.environ["AACP_CLAUDE_REGISTRY"] = self.reg
        self.assertEqual(importlib.reload(contours).REGISTRY, self.reg)

    def test_without_a_registry_path_only_the_environment_remains(self):
        self.contour()
        contours.REGISTRY = ""

        self.assertEqual(contours.config_dirs(), [self.home])
        self.assertEqual(contours.prefixes(), [])

    def test_a_contour_is_seen_without_restarting_the_service(self):
        before = agent.sessions_dirs()
        work = self.contour()
        self.assertEqual(before, [os.path.join(self.home, "sessions")])
        self.assertEqual(agent.sessions_dirs(),
                         [os.path.join(self.home, "sessions"),
                          os.path.join(work, "sessions")])

    def test_a_contour_without_its_directory_is_skipped(self):
        with open(self.reg, "w", encoding="utf-8") as f:
            f.write("acme | /srv/proj/Acme/ | %s | -\n"
                    % os.path.join(self.root, "no-such-dir"))
        self.assertEqual(contours.config_dirs(), [self.home])

    def test_comments_and_scraps_in_the_registry_do_not_break_the_parse(self):
        work = self.contour()
        with open(self.reg, "a", encoding="utf-8") as f:
            f.write("\n# tail\ngarbage with no fields\n")
        self.assertIn(work, contours.config_dirs())

    def test_a_substituted_directory_overrides_the_registry(self):
        self.contour()
        agent.CLAUDE_SESSIONS = os.path.join(self.root, "substituted")
        self.assertEqual(agent.sessions_dirs(),
                         [os.path.join(self.root, "substituted")])

    def test_the_personal_contour_comes_first(self):
        work = self.contour()
        self.assertEqual(agent.limits_paths(),
                         [os.path.join(self.home, "rate-limits.json"),
                          os.path.join(work, "rate-limits.json")])

    def registry(self, *rows):
        with open(self.reg, "w", encoding="utf-8") as f:
            f.write("# profile | prefix | config | token\n")
            for name, conf, token in rows:
                f.write("%s | /srv/proj/%s/ | %s | %s\n" % (name, name, conf, token))
            f.write("personal | * | %s | -\n" % self.home)

    def test_the_personal_profile_uses_the_built_in_authorization(self):
        self.registry()
        by = {p["name"]: p for p in contours.described()}
        self.assertEqual(by["personal"]["auth"], "builtin")

    def test_a_profile_with_its_own_token(self):
        work = self.contour()
        token = os.path.join(self.root, "work.age")
        open(token, "w").close()
        self.registry(("work", work, token))
        by = {p["name"]: p for p in contours.described()}
        self.assertEqual(by["work"]["auth"], "token")

    def test_a_token_that_is_declared_but_missing_is_trouble(self):
        work = self.contour()
        self.registry(("work", work, os.path.join(self.root, "no-such.age")))
        by = {p["name"]: p for p in contours.described()}
        self.assertEqual(by["work"]["auth"], "missing")

    def test_without_a_registry_only_the_personal_one_is_described(self):
        self.assertEqual([(p["name"], p["configDir"], p["auth"]) for p in contours.described()],
                         [("personal", self.home, "builtin")])

    def test_a_profile_without_its_directory_is_not_described(self):
        self.registry(("acme", os.path.join(self.root, "no-such-directory"), "-"))
        self.assertEqual([p["name"] for p in contours.described()], ["personal"])

    def settings(self, config_dir, hooks):
        with open(os.path.join(config_dir, "settings.json"), "w", encoding="utf-8") as f:
            json.dump({"hooks": hooks}, f)

    def test_the_personal_contour_is_not_compared_with_itself(self):
        work = self.contour()
        self.settings(self.home, {"PreToolUse": "x"})
        self.settings(work, {"PreToolUse": "x"})
        self.registry(("work", work, "-"))
        by = {p["name"]: p for p in contours.described()}
        self.assertNotIn("hooks", by["personal"])

    def test_identical_hooks_are_same(self):
        work = self.contour()
        hooks = {"PreToolUse": [{"matcher": "AskUserQuestion", "hooks": [
            {"type": "command", "command": "python3 agent/ask-hook.py"}]}]}
        self.settings(self.home, hooks)
        self.settings(work, dict(hooks))
        self.registry(("work", work, "-"))
        by = {p["name"]: p for p in contours.described()}
        self.assertEqual(by["work"]["hooks"], "same")

    def test_hooks_that_differ_are_diverged(self):
        work = self.contour()
        self.settings(self.home, {"PreToolUse": [{"matcher": "AskUserQuestion", "hooks": [
            {"type": "command", "command": "python3 agent/ask-hook.py"}]}]})
        self.settings(work, {"PreToolUse": [{"matcher": "AskUserQuestion", "hooks": [
            {"type": "command", "command": "python3 agent/ask-hook-old.py"}]}]})
        self.registry(("work", work, "-"))
        by = {p["name"]: p for p in contours.described()}
        self.assertEqual(by["work"]["hooks"], "diverged")

    def test_a_profile_without_settings_json_is_unknown(self):
        work = self.contour()
        self.settings(self.home, {"PreToolUse": "x"})
        self.registry(("work", work, "-"))
        by = {p["name"]: p for p in contours.described()}
        self.assertEqual(by["work"]["hooks"], "unknown")

    def test_the_account_says_three_keys_and_never_its_environment(self):
        work = self.contour()
        with open(os.path.join(work, "settings.json"), "w", encoding="utf-8") as f:
            json.dump({"model": "opus[1m]", "effortLevel": "xhigh", "permissions": {"defaultMode": "auto"},
                       "env": {"CLAUDE_CODE_OAUTH_TOKEN": "secret"}, "theme": "dark"}, f)
        self.registry(("work", work, "-"))
        by = {p["name"]: p for p in contours.described()}
        self.assertEqual(by["work"]["account"], {"model": "opus[1m]", "effort": "xhigh", "permissionMode": "auto"})
        self.assertNotIn("secret", json.dumps(contours.described()))

    def test_the_context_guard_is_seen_where_the_account_runs_it(self):
        work = self.contour()
        self.settings(self.home, {"Stop": [{"hooks": [
            {"type": "command", "command": "python3 /srv/proj/aacpanel/deploy/claude/context-guard.py"}]}]})
        self.settings(work, {"Stop": [{"hooks": [{"type": "command", "command": "bash cost.sh"}]}]})
        self.registry(("work", work, "-"))
        by = {p["name"]: p for p in contours.described()}
        self.assertTrue(by["personal"]["contextGuard"])
        self.assertFalse(by["work"]["contextGuard"])

    def test_a_broken_settings_json_of_a_profile_is_unknown(self):
        work = self.contour()
        self.settings(self.home, {"PreToolUse": "x"})
        with open(os.path.join(work, "settings.json"), "w", encoding="utf-8") as f:
            f.write("{not json")
        self.registry(("work", work, "-"))
        by = {p["name"]: p for p in contours.described()}
        self.assertEqual(by["work"]["hooks"], "unknown")

    def test_without_settings_json_the_personal_one_is_unknown_too(self):
        work = self.contour()
        self.settings(work, {"PreToolUse": "x"})
        self.registry(("work", work, "-"))
        by = {p["name"]: p for p in contours.described()}
        self.assertEqual(by["work"]["hooks"], "unknown")

    def rate(self, config, five, seven, at=None):
        import time
        with open(os.path.join(config, "rate-limits.json"), "w", encoding="utf-8") as f:
            json.dump({"at": at if at is not None else int(time.time()),
                       "fiveHour": {"pct": five}, "sevenDay": {"pct": seven}}, f)

    def test_limits_are_returned_per_contour(self):
        work = self.contour()
        self.rate(self.home, 12, 62)
        self.rate(work, 3, 40)

        got = agent.limits()
        self.assertEqual([c["profile"] for c in got["contours"]], ["personal", "work"])
        self.assertEqual([c["fiveHour"]["pct"] for c in got["contours"]], [12, 3])

    def test_limits_name_the_config_directory_of_the_contour(self):
        work = self.contour()
        self.rate(self.home, 12, 62)
        self.rate(work, 3, 40)

        by = {c["profile"]: c for c in agent.limits()["contours"]}
        self.assertEqual(by["personal"]["configDir"], self.home)
        self.assertEqual(by["work"]["configDir"], work)
        self.assertEqual(agent.limits()["configDir"], self.home)

    def test_a_snapshot_substituted_by_one_file_is_the_personal_contour(self):
        self.rate(self.home, 12, 62)
        agent.LIMITS_PATH = os.path.join(self.home, "rate-limits.json")

        got = agent.limits()
        self.assertEqual([c["profile"] for c in got["contours"]], ["personal"])
        self.assertEqual(got["contours"][0]["configDir"], self.home)

    def test_a_contour_without_a_limits_snapshot_is_not_shown(self):
        self.contour()
        self.rate(self.home, 12, 62)

        got = agent.limits()
        self.assertEqual([c["profile"] for c in got["contours"]], ["personal"])

    def test_the_first_contour_is_duplicated_at_the_root(self):
        work = self.contour()
        self.rate(self.home, 12, 62)
        self.rate(work, 3, 40)

        got = agent.limits()
        self.assertEqual(got["fiveHour"]["pct"], 12)
        self.assertEqual(got["sevenDay"]["pct"], 62)

    def test_the_age_is_counted_for_each_contour_on_its_own(self):
        import time
        work = self.contour()
        self.rate(self.home, 12, 62)
        self.rate(work, 3, 40, at=int(time.time()) - 7200)

        by = {c["profile"]: c for c in agent.limits()["contours"]}
        self.assertLess(by["personal"]["ageSec"], 60)
        self.assertGreater(by["work"]["ageSec"], 3000)

    def test_limits_with_no_snapshots_at_all_are_nothing(self):
        self.contour()
        self.assertIsNone(agent.limits())

    def transcript(self, config, project, uuid, body="{}\n"):
        d = os.path.join(config, "projects", project)
        os.makedirs(d, exist_ok=True)
        path = os.path.join(d, uuid + ".jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write(body)
        return path

    def test_the_feed_opens_a_conversation_of_a_work_contour(self):
        work = self.contour()
        want = self.transcript(work, "-srv-proj-Labs-site", UUID_A)
        self.assertEqual(chat.transcript_path(UUID_A), want)

    def test_the_archive_is_cut_by_profile(self):
        work = self.contour()
        self.transcript(self.home, "-home-u", UUID_A)
        self.transcript(work, "-srv-proj-Labs-site", UUID_B)
        index = archive.Index(os.path.join(self.root, "index.json"))
        personal_ids = {row["sessionId"] for row in index.page(limit=10, profile="personal")["rows"]}
        work_ids = {row["sessionId"] for row in index.page(limit=10, profile="work")["rows"]}
        every_id = {row["sessionId"] for row in index.page(limit=10)["rows"]}
        self.assertEqual(personal_ids, {UUID_A})
        self.assertEqual(work_ids, {UUID_B})
        self.assertEqual(every_id, {UUID_A, UUID_B},
                         "a request that names no contour asks for the archive of the machine")

    def test_the_name_of_a_live_session_of_a_work_contour_is_remembered(self):
        work = self.contour()
        with open(os.path.join(work, "sessions", "4242.json"), "w", encoding="utf-8") as f:
            json.dump({"sessionId": UUID_B, "name": "site", "cwd": "/srv/proj/Labs/site"}, f)
        index = archive.Index(os.path.join(self.root, "index.json"))
        index.observe()
        self.assertEqual(index.names.get(UUID_B), "site")

    def test_the_snapshot_names_the_contour_of_every_session(self):
        work = self.contour()
        self.transcript(work, "-srv-proj-Labs-site", UUID_B)
        with open(os.path.join(work, "sessions", "4247.json"), "w", encoding="utf-8") as f:
            json.dump({"sessionId": UUID_B, "name": "site", "cwd": "/srv/proj/Labs/site"}, f)

        transcript = os.path.join(work, "projects", "-srv-proj-Labs-site", UUID_B + ".jsonl")
        self.addCleanup(setattr, agent.ctx, "sessions", agent.ctx.sessions)
        agent.ctx.sessions = lambda: {
            "sessions": [{"session": "site", "sessionId": UUID_B, "transcript": transcript}],
            "notes": [],
        }

        found = agent.sessions()["sessions"]
        self.assertEqual([s.get("profile") for s in found], ["work"])

    def test_a_live_session_knows_its_contour(self):
        work = self.contour()
        with open(os.path.join(work, "sessions", "4244.json"), "w", encoding="utf-8") as f:
            json.dump({"sessionId": UUID_B, "name": "site", "cwd": "/srv/proj/Labs/site"}, f)
        with open(os.path.join(self.home, "sessions", "4245.json"), "w", encoding="utf-8") as f:
            json.dump({"sessionId": UUID_A, "name": "task", "cwd": "/srv/proj/Task/task"}, f)

        found = agent.session_profiles()
        self.assertEqual(found.get(UUID_B), ("work", work))
        self.assertEqual(found.get(UUID_A), ("personal", self.home))

    def test_a_contour_without_a_name_is_not_attached_to_a_session(self):
        agent.CLAUDE_SESSIONS = os.path.join(self.home, "sessions")
        with open(os.path.join(self.home, "sessions", "4246.json"), "w", encoding="utf-8") as f:
            json.dump({"sessionId": UUID_A, "name": "aacpanel", "cwd": "/opt/x"}, f)
        self.assertEqual(agent.session_profiles(), {})

    def test_the_team_roster_is_taken_from_the_contour_of_the_session(self):
        work = self.contour()
        team = os.path.join(work, "teams", "session-" + UUID_B[:8])
        os.makedirs(team)
        with open(os.path.join(team, "config.json"), "w", encoding="utf-8") as f:
            json.dump({"members": [{"name": "team-lead"}, {"name": "reviewer"}]}, f)
        path = self.transcript(work, "-srv-proj-Labs-site", UUID_B)
        self.assertEqual(sesstate.team_names(path), {"team-lead", "reviewer"})


if __name__ == "__main__":
    unittest.main()
