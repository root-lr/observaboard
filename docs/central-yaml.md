# Référence : central.yaml

`central.yaml` contient les réglages du serveur central, y compris les secrets des canaux de notification (fichier en mode 0600). Après chaque modification :

```bash
sudo observa central reload
```

`reload` régénère les configurations et recharge à chaud ce qui peut l'être : Caddy, Prometheus et Alertmanager. Loki, Tempo et Grafana redémarrent si leur configuration change.

```yaml
version: 1
mode: server                 # server | local (all-in-one)
domain: observa.example.com  # ou ip: 203.0.113.7 (certificat interne)
email: vous@example.com      # contact Let's Encrypt (optionnel)
admin_user: admin
retention:
  metrics: 15d               # Prometheus
  logs: 168h                 # Loki, multiple de 24h
  traces: 72h                # Tempo
notifications:
  smtp:
    smarthost: smtp.example.com:587
    from: alertes@example.com
    username: alertes@example.com
    password: "…"
    require_tls: true
  channels:
    email-moi:      { type: email, to: moi@example.com }
    slack-wulo:     { type: slack, webhook_url: "https://hooks.slack.com/services/…", channel: "#alertes" }
    telegram-ops:   { type: telegram, bot_token: "123456:ABC…", chat_id: -1001234567890 }
    mon-webhook:    { type: webhook, url: "https://example.com/hook" }
  default: [email-moi]
  heartbeat_url: https://hc-ping.com/<uuid>
```

## Routage des alertes

| Alerte | Destinataires |
| --- | --- |
| Alerte d'un service dont le projet déclare `notifications.critical` / `warning` | les canaux listés pour sa sévérité |
| Alerte d'un service sans canal déclaré, alerte serveur (disque, mémoire, agent silencieux), alerte du central | `default` |
| `Watchdog` (toujours active) | `heartbeat_url` toutes les minutes, sinon ignorée |

Règles d'inhibition :
- une alerte critique masque les avertissements du même service ;
- un agent silencieux masque les alertes « service arrêté » de son serveur.

Vérifier le routage sans attendre une vraie panne :

```bash
sudo observa alert test                                         # canaux par défaut
sudo observa alert test --notify-critical telegram-ops          # canal d'un projet
```

## Silences

Pour couper temporairement une alerte (maintenance, service retiré volontairement), allez dans Grafana, *Alerting › Silences*, puis choisissez la source **Alertmanager**. Elle est provisionnée automatiquement.

## Heartbeat externe

Si Prometheus ou Alertmanager tombent, plus aucune alerte ne part. Créez un check chez un service externe comme healthchecks.io ou un Uptime Kuma en mode push, avec une période d'une minute et une tolérance de 5 minutes, puis renseignez son URL dans `heartbeat_url`. Son silence signale la panne du central.

## Telegram

1. Créez un bot avec @BotFather et récupérez son `bot_token`.
2. Ajoutez le bot au groupe destinataire.
3. Récupérez le `chat_id` du groupe (nombre négatif) via `https://api.telegram.org/bot<token>/getUpdates`.
4. Écrivez `chat_id` comme un nombre, sans guillemets.
