package action

// Status is what a session says about itself beyond what the snapshot of the
// host holds: the version of claude it runs, and on the stream the account it
// is signed in with, as claude named it at the handshake.
type Status struct {
	Transport string   `json:"transport"`
	Version   string   `json:"version,omitempty"`
	Account   *Account `json:"account,omitempty"`
}

// Account is who a session on the stream works as.
type Account struct {
	Email        string `json:"email,omitempty"`
	Organization string `json:"organization,omitempty"`
	Plan         string `json:"plan,omitempty"`
	Provider     string `json:"provider,omitempty"`
}
