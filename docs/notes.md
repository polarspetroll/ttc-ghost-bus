# Notes

- **Fallback match.** If many live vehicles carry a trip ID that is not in the static schedule, they get matched by route + direction + the scheduled start closest to when the vehicle first showed up (within 15 minutes). Direction is skipped when the feed doesnt send it. It runs every night in `sql/aggregate.sql` and only touches trip IDs that are not in the static schedule. Vehicles with no trip ID at all cant be matched and are ignored.
- **Only buses and streetcars are counted.** The loader keeps GTFS route types 0 and 3, because the subway is not in the bus feed and would show up as 100% ghosts.
- **Downtime.** An hour with fewer than 108 of the expected 120 successful polls is marked as downtime, and any trip whose window touches it is left out.
- **Holidays.** A weekday holiday runs a Sunday schedule but is still counted as a weekday by date.
- **Deploy from CI.** The security group only opens port 6443 to my IP, so the GitHub-hosted runner cant reach the cluster. The deploy job is skipped unless a `KUBECONFIG` secret is set; in practice deploy with `kubectl apply -k deploy/k8s` from my machine or use a self-hosted runner.
- **GHCR image.** New packages on GHCR are private. Either make the package public or add an `imagePullSecret` so k3s can pull it.
