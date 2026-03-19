<!-- title: FAQ & Troubleshooting -->
<!-- category: Support -->

# FAQ & Troubleshooting

## General Questions

### What databases are supported?

BackupManager supports **MySQL / MariaDB** and **PostgreSQL** out of the box.
MongoDB support is on the roadmap.

### Can I back up Windows servers?

Yes. BackupManager connects to Windows servers via WinRM.
See **Managing Servers → WinRM Connections** for setup instructions.

### How much storage do I need?

As a rough guide: plan for 1.5× the total size of your data for the first month,
then add ~10–20% per month for incremental changes.
Use the dashboard storage chart to monitor actual usage.

### Are backups encrypted?

Yes. Data at rest is encrypted with AES-256-GCM.
Each snapshot uses a unique key derived from the destination key.
Data in transit is encrypted via SSH (for file backups) or TLS (for HTTPS destinations).

## Backup Failures

### Backup failed: "connection refused"

- Verify the server is running and reachable
- Check that SSH (port 22) or WinRM (port 5985) is open in the firewall
- Use **Servers → Test Connection** to diagnose

### Backup failed: "permission denied"

The backup user does not have read access to one or more paths.
Fix on the target server:

```bash
# Add backupmanager to the required group
sudo usermod -aG www-data backupmanager
# Or grant read access to the directory
sudo chmod o+r /var/www/html
```

### Backup failed: "mysqldump: command not found"

Install the MySQL client tools on the target server:

```bash
sudo apt-get install -y mysql-client      # Debian/Ubuntu
sudo yum install -y mysql                 # RHEL/CentOS
```

### Backup shows status "Partial"

Some sources completed and others failed.
Click the run row in **Jobs → Run History** to see per-source status and logs.

## Scheduling Issues

### My job is not running at the expected time

- Check the server timezone in **Settings → General**
- Cron expressions use the server's local time, not UTC
- Verify the job is enabled (toggle in the job list)

### How do I pause a job temporarily?

Toggle the **Enabled** switch on the job row.
The job will not run while disabled; no snapshots will be deleted.

## Storage and Retention

### Old snapshots are not being deleted

Retention cleanup runs after each successful backup.
If a job keeps failing, old snapshots accumulate.
You can manually delete snapshots from the **Snapshots** page.

### Can I store backups on multiple destinations?

Yes. Go to **Destinations** to configure additional storage targets (S3, SFTP, local disk).
Each job can replicate to one or more destinations.

## Authentication

### I forgot my admin password

Reset it from the command line on the BackupManager server:

```bash
./backupmanager reset-password --user admin
# Follow the prompts to set a new password
```

### How do I add more users?

Multi-user support is on the roadmap.
Currently BackupManager uses a single admin account.

## Data Integrity

### What does an integrity check do?

BackupManager computes a SHA-256 checksum for every stored file at backup time.
The integrity check re-computes the checksum and compares it.
A mismatch indicates storage corruption or tampering.

### How often should I run integrity checks?

Monthly is a good baseline for most deployments.
You can schedule automatic checks under **Settings → Integrity**.
