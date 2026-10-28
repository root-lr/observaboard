# Dépendances embarquées

Ces paquets sont copiés tels quels depuis leurs dépôts d'origine, sans modification
autre que le chemin d'import de `blowfish` dans `bcrypt`. Ils sont embarqués pour que
`go build` fonctionne sans téléchargement (installation hors ligne, environnements
avec proxy restreint). Leurs licences d'origine sont conservées dans chaque dossier.

| Dossier | Origine | Version | Licence |
| --- | --- | --- | --- |
| `yaml/` | github.com/go-yaml/yaml (`gopkg.in/yaml.v3`) | v3.0.1 | MIT et Apache-2.0 |
| `bcrypt/` | github.com/golang/crypto (`golang.org/x/crypto/bcrypt`) | commit 1da6254a | BSD-3-Clause |
| `blowfish/` | github.com/golang/crypto (`golang.org/x/crypto/blowfish`) | commit 1da6254a | BSD-3-Clause |

Pour revenir à des modules Go classiques : `go get gopkg.in/yaml.v3 golang.org/x/crypto`,
remplacer les imports `internal/thirdparty/...`, puis supprimer ce dossier.
