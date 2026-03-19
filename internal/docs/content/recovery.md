<!-- title: Disaster Recovery -->
<!-- category: Recovery -->

# Disaster Recovery

BackupManager provides **recovery playbooks** — step-by-step restore procedures
generated automatically from your server configuration and most recent snapshot.

## Recovery Playbooks

A playbook is a structured document that walks an operator through restoring
a server from scratch, including:

- Required tools and packages
- Network and firewall configuration
- Directory structure restoration
- Database restore commands
- Service restart sequences

### Generating a Playbook

1. Go to **Recovery**
2. Click **Generate Playbook** next to the target server
3. BackupManager analyses the server configuration and latest snapshot
4. A playbook is created and displayed

Playbooks are regenerated automatically after each successful backup.
You can also trigger regeneration manually at any time.

### Editing a Playbook

Playbooks can be customised for your environment:

1. Open the playbook
2. Click **Edit**
3. Modify the steps as needed (Markdown format)
4. Click **Save**

Customisations are preserved across automatic regenerations.

## Step-by-Step Restore Procedure

### 1. Provision a Replacement Server

Spin up a new server with the same OS version as the original.
Note the IP address — you will need to update DNS records.

### 2. Install Required Tools

```bash
# Debian / Ubuntu
sudo apt-get install -y rsync openssh-server

# RHEL / CentOS
sudo yum install -y rsync openssh-server
```

### 3. Restore Directory Snapshots

Use `rsync` to push data from the BackupManager snapshot storage to the new server:

```bash
rsync -az --progress \
  /path/to/snapshot/var/www/ \
  backupmanager@new-server:/var/www/
```

Or use the **Download** button on any snapshot in the UI to get a `.tar.gz` archive.

### 4. Restore Databases

#### MySQL / MariaDB

```bash
# Transfer and import the dump
scp snapshot/myapp_db.sql.gz backupmanager@new-server:/tmp/
ssh backupmanager@new-server \
  "zcat /tmp/myapp_db.sql.gz | mysql -u root -p myapp_db"
```

#### PostgreSQL

```bash
scp snapshot/myapp_db.dump backupmanager@new-server:/tmp/
ssh backupmanager@new-server \
  "pg_restore -U postgres -d myapp_db /tmp/myapp_db.dump"
```

### 5. Verify Services

Start services and run smoke tests:

```bash
sudo systemctl start nginx mysql
curl -f http://localhost/healthz && echo "OK"
```

### 6. Update DNS

Point your domain to the new server IP.
Wait for TTL to expire (typically 5–60 minutes).

## Selecting a Snapshot for Restore

In **Recovery**, use the snapshot selector to choose the restore point:

- **Latest** — most recent successful snapshot (default)
- **Point-in-time** — browse the snapshot calendar and pick a specific date

## Testing Restores

Test your recovery procedure regularly:

1. Provision a test VM (same OS, isolated network)
2. Follow the playbook steps
3. Verify data integrity and service functionality
4. Document the recovery time objective (RTO) achieved

A successful restore test gives you confidence that your backups are valid.
