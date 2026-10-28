# ObservaHostDiskFull — disk almost full

**Severity:** warning · **Fires when:** a filesystem on the server has been more than 85% full for 10 minutes.

## What the alert means

Free space is shrinking. Beyond 95%, databases, Docker and logs start failing.

## User impact

None while space remains; then write errors, a stopped database, impossible deployments.

## Dashboards to open

1. **Servers**: *Disk usage* to see how fast it fills up.

## Useful queries

Forecast: will the disk be full within 24 h?

```promql
predict_linear(node_filesystem_avail_bytes{host="HOST", mountpoint="/"}[6h], 24 * 3600) < 0
```

On the server:

```bash
df -h
docker system df
sudo du -xh / --max-depth=2 2>/dev/null | sort -h | tail -20
```

## Likely causes

- **Unused Docker images and containers** piling up over deployments.
- Container **logs** without rotation.
- A growing **database**, local backups piling up.
- Central **data volumes** (if this is the central server: retention too long).

## Actions

1. Clean up Docker: `docker image prune -a` (unused images), `docker builder prune`.
2. Enable Docker log rotation (`max-size`, `max-file`).
3. Move or purge old backups.
4. On the central: reduce `retention` in `central.yaml`, then `observa central reload`.
5. Grow the disk if the growth is normal.

## Verify recovery

- *Disk usage* drops back below 85%.
