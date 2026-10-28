# Mac et Windows

Docker ne tourne nativement que sous Linux. Sur Mac et Windows, Docker Desktop crée une machine virtuelle Linux cachée, et vos conteneurs tournent dedans. ObservaBoard le détecte automatiquement : `observa doctor` le signale, et l'agent s'adapte.

## Ce qui fonctionne sous Docker Desktop

- `observa up --all-in-one --with-example` : central, agent, application d'exemple.
- Logs, métriques des conteneurs, traces OpenTelemetry (niveau 2), métriques HTTP, alertes, dashboards.
- `observa init` / `apply` sur vos projets.

## Ce qui change

- **Métriques de « l'hôte » :** ce sont celles de la VM Docker, pas de votre Mac ou PC. L'agent ne monte pas `/`, `/proc` ni `/var/lib/docker`, que Docker Desktop ne partage pas.
- **eBPF :** il dépend du noyau de la VM, sans garantie. Préférez le niveau 2 (OpenTelemetry), qui donne de toute façon plus d'informations.

## Tester eBPF à coup sûr

Utilisez une vraie VM Linux :

- **Mac :** OrbStack, Lima ou Colima.
- **Windows :** installez Docker Engine *dans* une distribution WSL2 (Ubuntu), plutôt que Docker Desktop, puis lancez `observa` depuis cette distribution.

Puis : `observa up --all-in-one --with-example --ebpf`.

## Windows

- **Binaire :** il est disponible (`observa-windows-amd64.exe`).
- **Commandes Docker :** utilisez PowerShell ou WSL2 pour lancer les commandes `docker compose` affichées par la CLI.
- **Données all-in-one :** elles sont stockées dans `%USERPROFILE%\.observa\local`.
