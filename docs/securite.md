# Sécurité

## Ce qui est exposé

- **Central :** seul Caddy publie des ports (80 et 443, ou `127.0.0.1:3000` en all-in-one). Prometheus, Loki, Tempo, Alertmanager et Grafana restent sur un réseau Docker interne.
- **Agents :** aucun port publié. Ils poussent vers le central. Le récepteur OTLP de l'agent n'est accessible que depuis le réseau Docker `observa`.

## Authentification des agents

- Chaque agent a un **token aléatoire de 256 bits**, au format `<agent>.<secret>`, créé par `observa token create`. Il n'est affiché qu'une fois.
- **Côté central**, seul un hachage bcrypt est conservé (`tokens.yaml`, 0600). Caddy vérifie identifiant et secret à chaque envoi (HTTP basic auth sur HTTPS).
- **Côté agent**, le secret est dans `.env` (0600), lu par variable d'environnement. Il n'est jamais écrit dans `config.alloy`.
- **Révocation :** `observa token revoke <agent>`. Elle est effective immédiatement : la commande recharge Caddy, qui monte son dossier de configuration et relit donc la nouvelle version.
- **Saisie du token :** préférez `--token-file <fichier>`, `--token-file -` (entrée standard) ou la variable `OBSERVA_AGENT_TOKEN` à `--token`, qui reste dans l'historique du shell et visible dans `ps`.
- **Coût bcrypt :** il est volontairement minimal. Le secret est aléatoire et long, donc un coût élevé n'ajouterait aucune protection et ralentirait chaque requête.

En mode `--ip`, le certificat vient de l'autorité interne de Caddy. Donnez-la aux agents avec `--ca-file`. N'utilisez `--insecure-skip-verify` que pour un test.

## Privilèges de l'agent

L'agent a besoin d'accès étendus à l'hôte, et il faut le savoir :

| Accès | Raison |
| --- | --- |
| socket Docker (lecture seule) | découverte des conteneurs et lecture de leurs logs. **Un accès au socket Docker équivaut à un accès root à l'hôte.** |
| conteneur `privileged` | exigé par cAdvisor pour lire les cgroups et les disques |
| `/`, `/proc`, `/sys`, `/var/lib/docker` en lecture seule | métriques de l'hôte et des conteneurs |
| `pid: host` (seulement avec `--ebpf`) | eBPF observe les processus de l'hôte |

N'installez l'agent que sur des serveurs dont vous êtes administrateur, et gardez son image à jour (`observa agent install`, avec une nouvelle version de la CLI).

## Isolation entre projets et entre agents (limites de la V1)

- **Réseau partagé :** sur un même serveur, tous les services surveillés rejoignent le réseau Docker `observa`, avec un alias prévisible (`<projet>-<env>-<service>`). Un conteneur d'un projet peut donc joindre la base d'un autre projet du même serveur. N'hébergez pas sur un même serveur des projets de clients qui ne doivent pas se voir, ou protégez chaque base par un mot de passe propre (ce qui est de toute façon nécessaire). Un réseau par projet est prévu en V2.
- **Labels déclarés par l'agent :** le central authentifie chaque agent, mais il ne contrôle pas les labels que l'agent envoie. Un agent compromis pourrait se faire passer pour un autre serveur, avec un faux label `host`, ou publier des seuils qui masquent les alertes d'un autre projet. Traitez chaque serveur équipé d'un agent comme de confiance. Le multi-tenant de la V2 imposera les labels côté central selon le token.

## Données sensibles

- **Logs :** ils sont collectés tels quels. Évitez de journaliser des secrets, des données de carte ou des mots de passe. Le masquage automatique de motifs est prévu en V2.
- **Traces :** elles contiennent les routes, les paramètres de route et les requêtes SQL. Les valeurs des requêtes SQL paramétrées ne sont pas enregistrées par défaut par les instrumentations OpenTelemetry.
- **Secrets :** ceux des bases de données (accès des exporters) restent sur le serveur de l'agent (`.env`, 0600, valeurs entre apostrophes pour que `$` et `#` soient lus tels quels). Ceux des canaux de notification restent sur le central, dont le dossier est en 0700. Aucun secret ne va dans `observa.yaml`.

## Grafana

- **Mot de passe :** le mot de passe admin est aléatoire et généré à l'installation. Changez-le dans Grafana après la première connexion. Le fichier `.env` n'est lu qu'au premier démarrage.
- **Accès :** l'inscription et l'accès anonyme sont désactivés.
