# The tailscale container

The panel exposed without a domain of its own, or a spare way in when there is
one: a tailnet node lives as a container in this very stack, `tailscale serve`
gives it a real certificate on `<node>.<tailnet>.ts.net` and proxies into the
compose network.

Two `serve` configurations:

- `serve-main.json` — the panel **without a domain**: tailscale is the main way
  in and proxies to the public listener `:8776`; `AACP_RP_ID` is the node name in
  the tailnet.
- `serve-leg.json` — the panel has a domain and tailscale is the fourth leg of
  the router, the one with the lowest priority; it proxies to its own listener
  `:8778` (`AACP_TS_ADDR`) so that the panel knows where the request came from.

Turning it on: `AACP_TAILSCALE=1` in `.env` (otherwise the container does not
start — `replicas: 0`), and logging in with a one-time `TS_AUTHKEY` in `.env`
before the container comes up, by that route only. The link in `docker compose
logs tailscale` looks like it works, but the confirmation never arrives:
`containerboot` waits a minute and brings `tailscaled` up again with a new node
key, leaving dead nodes in the admin console. MagicDNS and HTTPS have to be on in
the tailnet. Do not turn Funnel on: it puts the panel on the open internet.
