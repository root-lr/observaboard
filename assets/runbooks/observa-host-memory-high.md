# ObservaHostMemoryHigh — mémoire du serveur presque épuisée

**Sévérité :** avertissement · **Déclenchement :** plus de 90 % de la mémoire utilisée depuis 10 minutes.

## Ce que signifie l'alerte

Il reste peu de mémoire disponible. Le noyau va bientôt tuer des processus (OOM killer), souvent le plus gros : la base de données ou l'application.

## Impact pour les utilisateurs

Lenteurs (swap), puis redémarrages brutaux de services.

## Dashboards à ouvrir

1. **Serveurs** : *Mémoire utilisée* et *Mémoire des 10 conteneurs les plus gourmands*.

## Requêtes utiles

```promql
topk(5, sum by (container) (container_memory_working_set_bytes{host="HOST", container!=""}))
```

```bash
docker stats --no-stream
free -h
```

## Causes probables

- **Fuite mémoire** dans une application : sa courbe monte sans redescendre.
- **Trop de workers** (gunicorn, Celery) pour la taille du serveur.
- **Cache** de base de données dimensionné trop grand.

## Actions

1. Identifiez le conteneur qui grossit.
2. Fuite : redémarrez-le en attendant le correctif ; pour gunicorn, `--max-requests` recycle les workers.
3. Ajoutez des limites mémoire aux conteneurs (`mem_limit` / `deploy.resources.limits.memory`).
4. Réduisez le nombre de workers, ou augmentez la mémoire du serveur.

## Vérifier le retour à la normale

- La mémoire utilisée redescend sous 90 % et reste stable.
