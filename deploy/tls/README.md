# LAN certificate

The ACME client on the host puts the certificate for the LAN address of the panel
here (or into the directory named by `AACP_TLS_DIR`): `AACP_TLS_CERT` and
`AACP_TLS_KEY` are paths inside the container, and the directory is mounted as
`/tls` read-only. The service rereads the files by mtime, so no restart is needed
after a renewal.

The listener is off unless `AACP_LAN_ADDR` is set.

The installer offers `lego` alongside existing certificates. For a DNS provider
with an executable hook:

```sh
./install.sh --kit +lan --lan-cert lego \
  --tls-domain panel.example.org --tls-email owner@example.org \
  --tls-state ~/.config/aacpanel/lego --tls-provider exec \
  --tls-hook ~/bin/dns-hook
```

The hook receives `present|cleanup <fqdn> <value>` from lego. For Hetzner Cloud
DNS, select `--tls-provider hetzner --tls-zone example.org` and give
`--tls-token-file ~/.config/aacpanel/hetzner-dns.token` instead. The token file
must belong to the installer user and have mode `0600`; its contents never go
into a flag, the installer's settings, or the container. The shipped
`hetzner-dns-hook` removes only the current challenge's TXT value. Its local
validation makes no network request and does not read the token:

```sh
HETZNER_DNS_ZONE=example.org \
HETZNER_DNS_TOKEN_FILE=~/.config/aacpanel/hetzner-dns.token \
  ./deploy/tls/hetzner-dns-hook --dry-run cleanup \
  _acme-challenge.panel.example.org. example-challenge
```

`aacpanel-lego.timer` checks daily and renews at thirty days. Existing valid
certificates and accounts can be adopted in their current lego directory;
the installer does not request a replacement during adoption. The settings
are kept in `~/.config/aacpanel/lan-tls.json`. The units are written through the
install manifest and taken back on uninstall; account and certificate data stay.
Use `./install.sh check` to inspect the certificate and timer, and
`systemctl --user status aacpanel-lego.service` for its last exit status.
The hook's output is suppressed because a user-supplied hook may print secrets.
