// internal/recovery/generator.go
package recovery

import "fmt"

// GeneratePlaybooks auto-generates disaster recovery playbooks for a server
// based on its configured backup sources.
func GeneratePlaybooks(server ServerInfo, sources []SourceInfo) []Playbook {
	var playbooks []Playbook

	// Partition sources by type
	var webSources, dbSources, configSources []SourceInfo
	for _, s := range sources {
		switch s.Type {
		case "web":
			webSources = append(webSources, s)
		case "database":
			dbSources = append(dbSources, s)
		case "config":
			configSources = append(configSources, s)
		}
	}

	hasWeb := len(webSources) > 0
	hasDB := len(dbSources) > 0
	hasConfig := len(configSources) > 0

	// 1. Full Server Recovery (always generated)
	playbooks = append(playbooks, generateFullServerPlaybook(server, webSources, dbSources, configSources))

	// 2. Single Database Restore — one playbook per DB source
	for _, src := range dbSources {
		playbooks = append(playbooks, generateSingleDatabasePlaybook(server, src))
	}

	// 3. Single Project Restore — one playbook per web source
	for _, src := range webSources {
		playbooks = append(playbooks, generateSingleProjectPlaybook(server, src))
	}

	// 4. Config Only Restore — if config sources exist
	if hasConfig {
		playbooks = append(playbooks, generateConfigOnlyPlaybook(server, configSources))
	}

	// 5. Certificate Restore — if nginx/certbot configs exist (web or config sources)
	if hasWeb || hasConfig {
		playbooks = append(playbooks, generateCertificatePlaybook(server))
	}

	// Assign sequential IDs (in-memory; DB will assign real IDs on save)
	for i := range playbooks {
		playbooks[i].ID = i + 1
		sid := server.ID
		playbooks[i].ServerID = &sid
	}

	_ = hasDB // suppress unused warning when only config sources present

	return playbooks
}

// generateFullServerPlaybook creates the complete server-restoration playbook.
func generateFullServerPlaybook(server ServerInfo, webSources, dbSources, configSources []SourceInfo) Playbook {
	steps := []Step{
		{
			Order:       1,
			Title:       "Provision a new server",
			Description: "Boot a fresh Linux server with the same distribution. Ensure SSH access is available.",
			Command:     "ssh root@<NEW_SERVER_IP>",
			Verify:      "uname -a",
			Notes:       "Use the same OS version as the original server when possible.",
		},
		{
			Order:       2,
			Title:       "Install required packages",
			Description: "Install all base packages needed by the services that were running on the original server.",
			Command:     "apt-get update && apt-get install -y nginx mysql-server nodejs npm certbot python3-certbot-nginx",
			Verify:      "nginx -v && mysql --version && node --version",
		},
	}

	order := 3

	// Config restore steps
	if len(configSources) > 0 {
		steps = append(steps, Step{
			Order:       order,
			Title:       "Restore configuration files",
			Description: "Locate the latest config snapshot and restore NGINX, PM2, and service configuration files.",
			Command:     fmt.Sprintf("SNAPSHOT=$(ls -t /backups/%s/config/ | head -1)\ntar -xzf /backups/%s/config/$SNAPSHOT -C /", server.Name, server.Name),
			Verify:      "nginx -t && systemctl status nginx",
			Notes:       "Adjust the backup path to match your actual snapshot storage location.",
		})
		order++
	}

	// DB restore steps
	for _, src := range dbSources {
		dbName := src.DBName
		if dbName == "" {
			dbName = src.Name
		}
		steps = append(steps, Step{
			Order:       order,
			Title:       fmt.Sprintf("Restore database: %s", dbName),
			Description: fmt.Sprintf("Find the latest snapshot for database '%s', decompress it and import into MySQL.", dbName),
			Command:     fmt.Sprintf("SNAPSHOT=$(ls -t /backups/%s/db/%s/ | head -1)\ngunzip -c /backups/%s/db/%s/$SNAPSHOT | mysql -u root %s", server.Name, dbName, server.Name, dbName, dbName),
			Verify:      fmt.Sprintf("mysql -u root -e 'SHOW TABLES;' %s", dbName),
		})
		order++
	}

	// Web restore steps
	for _, src := range webSources {
		srcPath := src.SourcePath
		if srcPath == "" {
			srcPath = "/var/www/" + src.Name
		}
		steps = append(steps, Step{
			Order:       order,
			Title:       fmt.Sprintf("Restore web project: %s", src.Name),
			Description: fmt.Sprintf("Sync the latest snapshot for '%s' back to its original location.", src.Name),
			Command:     fmt.Sprintf("SNAPSHOT=$(ls -t /backups/%s/web/%s/ | head -1)\nrsync -avz /backups/%s/web/%s/$SNAPSHOT/ %s/", server.Name, src.Name, server.Name, src.Name, srcPath),
			Verify:      fmt.Sprintf("ls -la %s", srcPath),
		})
		order++
	}

	// Restart services
	steps = append(steps, Step{
		Order:       order,
		Title:       "Restart all services",
		Description: "Start NGINX, MySQL and any PM2-managed Node.js processes.",
		Command:     "systemctl restart nginx mysql\npm2 resurrect",
		Verify:      "systemctl is-active nginx && systemctl is-active mysql && pm2 list",
	})

	return Playbook{
		Title:    fmt.Sprintf("Full Server Recovery — %s", server.Name),
		Scenario: "full_server",
		Steps:    steps,
	}
}

// generateSingleDatabasePlaybook creates a playbook for restoring a single database.
func generateSingleDatabasePlaybook(server ServerInfo, src SourceInfo) Playbook {
	dbName := src.DBName
	if dbName == "" {
		dbName = src.Name
	}

	steps := []Step{
		{
			Order:       1,
			Title:       "Identify the snapshot to restore",
			Description: fmt.Sprintf("List available snapshots for database '%s' and choose the desired one.", dbName),
			Command:     fmt.Sprintf("ls -lt /backups/%s/db/%s/", server.Name, dbName),
			Notes:       "Choose the snapshot filename from the list above. Note the YYYYMMDD date in the name.",
		},
		{
			Order:       2,
			Title:       "Stop application connections",
			Description: "Prevent new connections to the database while restoring to avoid conflicts.",
			Command:     fmt.Sprintf("mysql -u root -e \"REVOKE ALL PRIVILEGES ON %s.* FROM '<app_user>'@'localhost'; FLUSH PRIVILEGES;\"", dbName),
			Notes:       "Replace <app_user> with the actual database user.",
		},
		{
			Order:       3,
			Title:       "Drop and recreate the database",
			Description: fmt.Sprintf("Clear the existing database '%s' before importing.", dbName),
			Command:     fmt.Sprintf("mysql -u root -e 'DROP DATABASE IF EXISTS %s; CREATE DATABASE %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;'", dbName, dbName),
			Verify:      fmt.Sprintf("mysql -u root -e 'SHOW DATABASES;' | grep %s", dbName),
		},
		{
			Order:       4,
			Title:       "Decompress and import the snapshot",
			Description: "Use the snapshot identified in step 1 to restore the database.",
			Command:     fmt.Sprintf("SNAPSHOT=<snapshot_filename>.sql.gz\ngunzip -c /backups/%s/db/%s/$SNAPSHOT | mysql -u root %s", server.Name, dbName, dbName),
			Verify:      fmt.Sprintf("mysql -u root -e 'SHOW TABLES;' %s", dbName),
		},
		{
			Order:       5,
			Title:       "Restore application privileges and restart",
			Description: "Re-grant database access to the application user and restart dependent services.",
			Command:     fmt.Sprintf("mysql -u root -e \"GRANT ALL PRIVILEGES ON %s.* TO '<app_user>'@'localhost'; FLUSH PRIVILEGES;\"\nsystemctl restart nginx", dbName),
			Notes:       "Replace <app_user> with the actual database user.",
		},
	}

	return Playbook{
		Title:    fmt.Sprintf("Single Database Restore — %s", dbName),
		Scenario: "single_database",
		Steps:    steps,
	}
}

// generateSingleProjectPlaybook creates a playbook for restoring a single web project.
func generateSingleProjectPlaybook(server ServerInfo, src SourceInfo) Playbook {
	srcPath := src.SourcePath
	if srcPath == "" {
		srcPath = "/var/www/" + src.Name
	}

	steps := []Step{
		{
			Order:       1,
			Title:       "Identify the snapshot to restore",
			Description: fmt.Sprintf("List available snapshots for project '%s' and choose the desired one.", src.Name),
			Command:     fmt.Sprintf("ls -lt /backups/%s/web/%s/", server.Name, src.Name),
			Notes:       "Select a snapshot directory from the listing above.",
		},
		{
			Order:       2,
			Title:       "Create a backup of current files",
			Description: fmt.Sprintf("Before overwriting, create a safety copy of the current '%s' directory.", srcPath),
			Command:     fmt.Sprintf("cp -r %s %s.bak.$(date +%%Y%%m%%d%%H%%M%%S)", srcPath, srcPath),
			Notes:       "Skip this step if the directory does not exist on the target server.",
		},
		{
			Order:       3,
			Title:       "Restore project files via rsync",
			Description: fmt.Sprintf("Sync the chosen snapshot back to '%s'.", srcPath),
			Command:     fmt.Sprintf("SNAPSHOT=<snapshot_dir>\nrsync -avz --delete /backups/%s/web/%s/$SNAPSHOT/ %s/", server.Name, src.Name, srcPath),
			Verify:      fmt.Sprintf("ls -la %s", srcPath),
		},
		{
			Order:       4,
			Title:       "Fix ownership and permissions",
			Description: "Ensure the web server user owns all restored files.",
			Command:     fmt.Sprintf("chown -R www-data:www-data %s\nfind %s -type d -exec chmod 755 {} \\;\nfind %s -type f -exec chmod 644 {} \\;", srcPath, srcPath, srcPath),
		},
		{
			Order:       5,
			Title:       "Reload NGINX",
			Description: "Apply the restored site configuration.",
			Command:     "nginx -t && systemctl reload nginx",
			Verify:      "curl -I http://localhost",
		},
	}

	return Playbook{
		Title:    fmt.Sprintf("Single Project Restore — %s", src.Name),
		Scenario: "single_project",
		Steps:    steps,
	}
}

// generateConfigOnlyPlaybook creates a playbook for restoring configuration files only.
func generateConfigOnlyPlaybook(server ServerInfo, configSources []SourceInfo) Playbook {
	steps := []Step{
		{
			Order:       1,
			Title:       "Identify the configuration snapshot",
			Description: "List available config snapshots and select the desired restore point.",
			Command:     fmt.Sprintf("ls -lt /backups/%s/config/", server.Name),
		},
		{
			Order:       2,
			Title:       "Backup current configuration",
			Description: "Archive existing configs before overwriting.",
			Command:     "tar -czf /tmp/config_backup_$(date +%Y%m%d%H%M%S).tar.gz /etc/nginx /etc/pm2 /etc/systemd/system",
		},
		{
			Order:       3,
			Title:       "Restore NGINX configuration",
			Description: "Restore NGINX site and server block configurations.",
			Command:     fmt.Sprintf("SNAPSHOT=<snapshot_filename>.tar.gz\ntar -xzf /backups/%s/config/$SNAPSHOT -C / etc/nginx", server.Name),
			Verify:      "nginx -t",
		},
	}

	order := 4
	for _, src := range configSources {
		steps = append(steps, Step{
			Order:       order,
			Title:       fmt.Sprintf("Restore config: %s", src.Name),
			Description: fmt.Sprintf("Restore configuration files for '%s'.", src.Name),
			Command:     fmt.Sprintf("SNAPSHOT=<snapshot_filename>.tar.gz\ntar -xzf /backups/%s/config/$SNAPSHOT -C / %s", server.Name, src.SourcePath),
		})
		order++
	}

	steps = append(steps, Step{
		Order:       order,
		Title:       "Reload services",
		Description: "Apply all restored configurations by reloading the affected services.",
		Command:     "systemctl daemon-reload && systemctl reload nginx",
		Verify:      "systemctl is-active nginx",
	})

	return Playbook{
		Title:    fmt.Sprintf("Config Only Restore — %s", server.Name),
		Scenario: "config_only",
		Steps:    steps,
	}
}

// generateCertificatePlaybook creates a playbook for restoring Let's Encrypt certificates.
func generateCertificatePlaybook(server ServerInfo) Playbook {
	steps := []Step{
		{
			Order:       1,
			Title:       "Identify certificate snapshot",
			Description: "List available certificate backups and select the restore point.",
			Command:     fmt.Sprintf("ls -lt /backups/%s/certs/", server.Name),
		},
		{
			Order:       2,
			Title:       "Stop NGINX",
			Description: "Stop NGINX to allow certificate renewal/replacement on port 80.",
			Command:     "systemctl stop nginx",
			Verify:      "systemctl is-active nginx || echo 'nginx stopped'",
		},
		{
			Order:       3,
			Title:       "Restore Let's Encrypt directory",
			Description: "Extract the certificate archive back to /etc/letsencrypt.",
			Command:     fmt.Sprintf("SNAPSHOT=<snapshot_filename>.tar.gz\ntar -xzf /backups/%s/certs/$SNAPSHOT -C /", server.Name),
			Verify:      "ls /etc/letsencrypt/live/",
		},
		{
			Order:       4,
			Title:       "Fix certificate permissions",
			Description: "Ensure correct ownership and permissions on the restored certificates.",
			Command:     "chown -R root:root /etc/letsencrypt\nchmod -R 755 /etc/letsencrypt/live\nchmod -R 600 /etc/letsencrypt/live/*/privkey.pem",
		},
		{
			Order:       5,
			Title:       "Verify and restart NGINX",
			Description: "Test the NGINX configuration with the restored certificates and start the service.",
			Command:     "nginx -t && systemctl start nginx",
			Verify:      "systemctl is-active nginx && certbot certificates",
		},
		{
			Order:       6,
			Title:       "Schedule certificate renewal (if missing)",
			Description: "Ensure the Certbot renewal timer is active so certificates auto-renew.",
			Command:     "systemctl enable certbot.timer && systemctl start certbot.timer",
			Verify:      "certbot renew --dry-run",
			Notes:       "Run this only if the renewal cron/timer was not restored by the config playbook.",
		},
	}

	return Playbook{
		Title:    fmt.Sprintf("Certificate Restore — %s", server.Name),
		Scenario: "certificates",
		Steps:    steps,
	}
}
