# Vendored dependencies

These packages are copied as is from their original repositories, with no change
other than the `blowfish` import path in `bcrypt`. They are vendored so that
`go build` works without downloads (offline installs, environments with a
restricted proxy). Their original licenses are kept in each directory.

| Directory | Origin | Version | License |
| --- | --- | --- | --- |
| `yaml/` | github.com/go-yaml/yaml (`gopkg.in/yaml.v3`) | v3.0.1 | MIT and Apache-2.0 |
| `bcrypt/` | github.com/golang/crypto (`golang.org/x/crypto/bcrypt`) | commit 1da6254a | BSD-3-Clause |
| `blowfish/` | github.com/golang/crypto (`golang.org/x/crypto/blowfish`) | commit 1da6254a | BSD-3-Clause |

To go back to regular Go modules: `go get gopkg.in/yaml.v3 golang.org/x/crypto`,
replace the `internal/thirdparty/...` imports, then delete this directory.
