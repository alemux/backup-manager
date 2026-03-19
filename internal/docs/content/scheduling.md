<!-- title: Scheduling Backups -->
<!-- category: Backups -->

# Scheduling Backups

BackupManager uses cron expressions to schedule backup jobs.
Each job runs on a configurable schedule and applies retention and bandwidth policies.

## Creating a Job

1. Go to **Jobs → Create Job**
2. Select the **server** and one or more **sources**
3. Choose a **schedule** (preset or custom cron)
4. Set a **retention policy**
5. Optionally configure a **bandwidth limit**
6. Click **Save**

## Cron Expressions

BackupManager uses standard 5-field cron syntax:

```
┌───────────── minute (0–59)
│ ┌───────────── hour (0–23)
│ │ ┌───────────── day of month (1–31)
│ │ │ ┌───────────── month (1–12)
│ │ │ │ ┌───────────── day of week (0–6, Sunday=0)
│ │ │ │ │
* * * * *
```

### Common Presets

| Expression | Meaning |
| ---------- | ------- |
| `0 2 * * *` | Every day at 02:00 |
| `0 2 * * 0` | Every Sunday at 02:00 |
| `0 */6 * * *` | Every 6 hours |
| `0 2 1 * *` | First day of each month at 02:00 |
| `*/15 * * * *` | Every 15 minutes |

The UI provides a schedule picker with common presets.
You can also type a custom expression directly.

## Retention Policies

Retention policies control how long snapshots are kept.
BackupManager supports **grandfathering** — keeping progressively fewer snapshots further back in time.

| Policy | Example |
| ------ | ------- |
| Keep last N | Keep the last 30 snapshots |
| Keep N days | Keep snapshots from the last 30 days |
| Grandfather | 7 daily + 4 weekly + 12 monthly |

The default policy is **30 days**.
Expired snapshots are deleted automatically after each successful run.

### Grandfather-Father-Son (GFS)

```
Daily:   keep last 7
Weekly:  keep last 4 (Sunday snapshots)
Monthly: keep last 12 (first Sunday of each month)
```

Enable GFS in the job settings under **Retention → Grandfather-Father-Son**.

## Bandwidth Limits

To avoid saturating your network during business hours, set a bandwidth limit:

- **Limit (MB/s)** — maximum transfer rate (applies to both upload and download)
- **Schedule** — apply the limit only during certain hours (e.g. business hours only)

Example: limit to 10 MB/s between 09:00 and 18:00 on weekdays.

## Manual Triggers

Any job can be triggered manually from the **Jobs** list:

1. Find the job row
2. Click the **Run Now** button (play icon)
3. Monitor progress in **Jobs → Run History**

## Run History

Each job run records:

- Status (running, completed, failed)
- Duration
- Bytes transferred
- Error messages and logs

View logs by clicking a run row in the history table.
