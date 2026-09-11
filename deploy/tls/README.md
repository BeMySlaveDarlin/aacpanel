# LAN certificate

The ACME client on the host puts the certificate for the LAN address of the panel
here (or into the directory named by `AACP_TLS_DIR`): `AACP_TLS_CERT` and
`AACP_TLS_KEY` are paths inside the container, and the directory is mounted as
`/tls` read-only. The service rereads the files by mtime, so no restart is needed
after a renewal.

An empty directory means there is no LAN listener, and that is the default.
