# open-webui-backup

Secure backup and restore tool for Open WebUI with mandatory age encryption.

## Features

- **Unified Commands** - Simple backup/restore/purge with selective type filtering
- **Mandatory Encryption** - All backups secured with age public key cryptography
- **Web Interface** - Built-in UI for backup/restore operations with real-time progress
- **Selective Operations** - Filter specific data types (knowledge, models, tools, prompts, files, chats)
- **Team Workflows** - Multi-recipient encryption for shared access
- **Safe Deletion** - Purge command with dry-run mode and confirmation prompts

## Installation

The project provides two separate commands:

- **`owuicli`** - Command-line tool for backup/restore operations.
- **`owuiback`** - Web server for dashboard-based operations.

```bash
# Install CLI tool only (no web frontend required)
go install github.com/vosiander/open-webui-backup/cmd/owuicli@latest

# Install web server (includes web frontend)
go install github.com/vosiander/open-webui-backup/cmd/owuiback@latest

# Or build from source
go build -o owuicli ./cmd/owuicli
go build -o owuiback ./cmd/owuiback

# Or use Task (builds both)
task build
```

## Quick Start

### Simplified Workflow (Recommended)

The new `full-backup` and `verify` commands handle age key management automatically:

```bash
# 1. Set up environment
export OPEN_WEBUI_URL="https://your-instance.com"
export OPEN_WEBUI_API_KEY="sk-your-api-key"

# 2. Create backup (generates keys automatically)
owuicli full-backup --path ./backups

# 3. Verify backup
owuicli verify --path ./backups

# 4. Restore backup (when needed)
owuicli restore --file ./backups/backup-*.zip.age \
    --decrypt-identity ./backups/identity.txt
```

### Advanced Workflow (Manual Key Management)

```bash
# 1. Generate Age Keys (One-Time)
owuicli new-identity --path ./my-keys
# Or use age-keygen:
# age-keygen -o ~/.age/identity.txt
# age-keygen -y ~/.age/identity.txt > ~/.age/recipient.txt

# 2. Configure Environment
export OPEN_WEBUI_URL="https://your-instance.com"
export OPEN_WEBUI_API_KEY="sk-your-api-key"
export OWUI_ENCRYPTED_RECIPIENT=$(cat ./my-keys/recipient.txt)
export OWUI_DECRYPT_IDENTITY="./my-keys/identity.txt"

# 3. Create backup
owuicli backup --out ./backups/full.zip

# 4. Restore backup
owuicli restore --file ./backups/full.zip.age

# 5. Web interface (separate command)
owuiback serve
# Open http://localhost:8080
```

## Commands

### CLI Commands (owuicli)

All backup, restore, and management operations are available through the `owuicli` command:

#### new-identity

Generate a new age identity keypair and save to files.

```bash
# Generate new identity in specified directory
owuicli new-identity --path ./my-keys

# Identity files created:
# - identity.txt (private key, 600 permissions)
# - recipient.txt (public key)
```

**Flags:**
- `--path` - Directory to save identity files (required)

**Notes:**
- Will not overwrite existing identity files
- Private key saved with restrictive 600 permissions
- Prints example environment variable commands

#### full-backup

Create a backup with automatic age encryption and identity management.

```bash
# Full backup (generates keys if needed)
owuicli full-backup --path ./backups

# Selective backup
owuicli full-backup --path ./backups --knowledge --models
owuicli full-backup --path ./backups --prompts --tools

# All files in same directory:
# - identity.txt (created if missing)
# - recipient.txt (created if missing)
# - backup-YYYYMMDD-HHMMSS.zip.age
```

**Flags:**
- `--path` - Directory for identity files and backup output (required)
- `--prompts`, `--tools`, `--knowledge`, `--models`, `--files`, `--chats`, `--users`, `--groups`, `--feedbacks` - Selective types (default: all)

**Features:**
- Auto-generates identity keypair if missing
- Reuses existing identity if present
- Creates timestamped backup files
- Automatic encryption with generated keys
- No need to manage recipients manually

#### verify

Verify that a backup file can be decrypted and optionally validate its contents.

```bash
# Verify newest backup in directory
owuicli verify --path ./backups

# Verify specific backup file
owuicli verify --path ./backups --file backup-20240101-120000.zip.age

# Only check decryption (skip content validation)
owuicli verify --path ./backups --only-encryption

# Verify shows:
# - Decryption success/failure
# - Backup metadata (type, timestamp, version)
# - Item counts by type
```

**Flags:**
- `--path` - Directory containing identity.txt and backup files (required)
- `--file` - Specific backup file to verify (optional, auto-detects newest .age file)
- `--only-encryption` - Only verify decryption, skip content validation

**Features:**
- Auto-detects newest backup if --file not specified
- Validates ZIP structure and metadata
- Counts items by type
- Works with both encrypted and unencrypted backups
- Temporary files automatically cleaned up

#### decrypt

Decrypt all .age encrypted files in a directory using identity.txt.

```bash
# Decrypt all .age files in directory
owuicli decrypt --path ./backups

# Decrypt with force overwrite
owuicli decrypt --path ./backups --force

# Output example:
# Decrypting 3 file(s) from ./backups
#
# ✓ backup-20240101-120000.zip.age → backup-20240101-120000.zip
# ⊘ data.zip.age → skipped (data.zip already exists, use --force)
# ✓ archive.zip.age → archive.zip
#
# ──────────────────────────────────────────────────
# Summary: 2 decrypted, 1 skipped, 0 failed
```

**Flags:**
- `--path` - Directory containing identity.txt and .age files to decrypt (required)
- `--force` - Overwrite existing decrypted files (optional)

**Features:**
- Automatically finds all .age files in directory
- Uses identity.txt from the specified path
- Validates files are actually encrypted before decrypting
- Skips files where decrypted version already exists (unless --force)
- Removes .age extension from output files
- Provides detailed progress and summary statistics
- Continues on error, reporting failures at the end

**Use Cases:**
- Bulk decryption of backup archives
- Preparing encrypted files for manual inspection
- Decrypting files for use with other tools

#### backup

Create an encrypted backup of Open WebUI data.

```bash
# Full backup
owuicli backup --out ./backups/full.zip

# Selective backup
owuicli backup --out ./backups/kb.zip --knowledge
owuicli backup --out ./backups/pt.zip --prompts --tools

# Team backup (multiple recipients)
owuicli backup --out ./backups/team.zip \
    --encrypt-recipient age1alice... \
    --encrypt-recipient age1bob...
```

**Flags:**
- `--out`, `-o` - Output file path (required)
- `--encrypt-recipient` - Age public key (required, repeatable)
- `--prompts`, `--tools`, `--knowledge`, `--models`, `--files`, `--chats` - Selective types

#### restore

Restore data from an encrypted backup.

```bash
# Full restore
owuicli restore --file ./backups/full.zip.age

# Selective restore
owuicli restore --file ./backups/full.zip.age --knowledge --models

# With overwrite
owuicli restore --file ./backups/full.zip.age --overwrite
```

**Flags:**
- `--file`, `-f` - Input file path (required)
- `--decrypt-identity` - Path to age identity file (required, repeatable)
- `--overwrite` - Replace existing data (default: skip existing)
- `--prompts`, `--tools`, `--knowledge`, `--models`, `--files`, `--chats` - Selective types

#### purge

Safely delete data with dry-run and confirmation.

```bash
# Dry-run (shows what would be deleted)
owuicli purge
owuicli purge --chats --files

# Force deletion (requires typing "yes")
owuicli purge --force
owuicli purge --chats --force
```

**Flags:**
- `--force`, `-f` - Actually perform deletion (required for deletion)
- `--chats`, `--files`, `--models`, `--knowledge`, `--prompts`, `--tools` - Selective types

#### backup-database

Create a standalone encrypted backup of the PostgreSQL database.

```bash
# Backup database (requires POSTGRES_URL environment variable)
owuicli backup-database --out ./backups/db-backup.zip

# With specific PostgreSQL URL
owuicli backup-database --out ./backups/db-backup.zip \
    --postgres-url "postgresql://user:pass@host:5432/dbname"

# With encryption recipient
owuicli backup-database --out ./backups/db-backup.zip \
    --encrypt-recipient age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p
```

**Flags:**
- `--out`, `-o` - Output file path (required)
- `--postgres-url` - PostgreSQL connection URL (optional, uses POSTGRES_URL env var)
- `--encrypt-recipient` - Age public key for encryption (optional, uses OWUI_ENCRYPTED_RECIPIENT env var)

**Requirements:**
- PostgreSQL client tools (`pg_dump`, `psql`) must be installed
- Database connection must be accessible
- POSTGRES_URL environment variable or --postgres-url flag

**Output:**
- Encrypted `.zip.age` file containing:
  - `database/dump.sql` - Plain SQL dump
  - `database/metadata.json` - Backup metadata (timestamp, version, etc.)

**Notes:**
- Uses `pg_dump` with `--no-owner --no-privileges` flags for portability
- Database backup is independent of API-based data backups
- Can be integrated into full-backup with `--database` flag

#### restore-database

Restore PostgreSQL database from an encrypted backup.

```bash
# Restore database
owuicli restore-database --file ./backups/db-backup.zip.age \
    --decrypt-identity ./my-keys/identity.txt

# With clean restore (drops existing objects first)
owuicli restore-database --file ./backups/db-backup.zip.age \
    --decrypt-identity ./my-keys/identity.txt \
    --clean

# Create database if it doesn't exist
owuicli restore-database --file ./backups/db-backup.zip.age \
    --decrypt-identity ./my-keys/identity.txt \
    --create-db
```

**Flags:**
- `--file`, `-f` - Encrypted backup file (required)
- `--decrypt-identity` - Path to age identity file (optional, uses OWUI_DECRYPTION_IDENTITY env var)
- `--postgres-url` - PostgreSQL connection URL (optional, uses POSTGRES_URL env var)
- `--clean` - Drop existing objects before restore (use with caution)
- `--create-db` - Create database if it doesn't exist

**Requirements:**
- PostgreSQL client tools (`psql`) must be installed
- Target database must be accessible
- Valid decryption identity for the backup file

**Notes:**
- Restoration is mutually exclusive with API-based restoration
- Use `--clean` flag carefully as it drops existing objects
- Database restore does NOT restore API-based data (chats, knowledge bases, etc.)

#### purge-database

Show what would be deleted or actually delete all data from the PostgreSQL database.

```bash
# Dry-run (shows what would be deleted) - DEFAULT BEHAVIOR
owuicli purge-database

# Actual purge (permanently deletes all data)
owuicli purge-database --force

# With specific PostgreSQL URL
owuicli purge-database --postgres-url "postgresql://user:pass@host:5432/dbname" --force
```

**Flags:**
- `--force`, `-f` - Actually delete data (without this, only shows what would be deleted)
- `--postgres-url` - PostgreSQL connection URL (optional, uses POSTGRES_URL env var)

**Behavior:**
- **Without `--force`**: Dry-run mode - shows all tables, sequences, and views that would be deleted
- **With `--force`**: Permanently deletes all tables, sequences, and views in the database

**Safety Features:**
- Default is dry-run mode (safe to run without --force)
- Lists all objects that will be affected
- Connection test before any operations
- Clear warnings displayed in force mode

**Notes:**
- **EXTREMELY DANGEROUS when used with --force** - irreversibly deletes all database data
- Always run without --force first to see what will be deleted
- Create a backup before purging (recommended)
- Does not affect API-based data stored separately

### Database Integration with Full Backups

The `full-backup` and `backup` commands support optional database backup integration with **automatic detection**:

```bash
# Database backup is AUTO-ENABLED when POSTGRES_URL is set
export POSTGRES_URL="postgresql://user:pass@host:5432/dbname"
owuicli full-backup --path ./backups
# ✓ POSTGRES_URL detected, including database backup automatically

# Explicitly enable (when POSTGRES_URL is not in environment)
owuicli full-backup --path ./backups --database

# Works with selective backups too
owuicli backup --out ./backups/data.zip --knowledge --models
# ✓ POSTGRES_URL detected, including database backup automatically
```

**Auto-Enable Behavior:**
- Database backup is **automatically included** when `POSTGRES_URL` environment variable is detected
- No need to use `--database` flag if `POSTGRES_URL` is set
- You can still explicitly use `--database` flag if needed
- Fails gracefully with warning if PostgreSQL tools are not available

**Requirements:**
- `POSTGRES_URL` environment variable (for auto-enable)
- PostgreSQL client tools (`pg_dump`, `psql`) must be installed
- Database must be accessible

**Backup Structure:**
- Database dump is added to the same ZIP file as API data
- Database folder structure: `database/dump.sql` and `database/metadata.json`
- Backup fails gracefully if database backup fails (with warning)

**Restoration:**
When restoring a full backup that includes database data, use the `restore-database` command separately. The `restore` command will detect and notify you if database backup is present but skip database restoration (API data and database must be restored separately).

**Example Full Workflow:**
```bash
# 1. Set environment (database backup auto-enabled)
export POSTGRES_URL="postgresql://user:pass@localhost:5432/openwebui"
export OPEN_WEBUI_URL="https://your-instance.com"
export OPEN_WEBUI_API_KEY="sk-your-key"

# 2. Create full backup (database included automatically)
owuicli full-backup --path ./backups
# Output: ✓ POSTGRES_URL detected, including database backup automatically
#         ✓ Database backup included

# 3. Restore API data
owuicli restore --file ./backups/backup-*.zip.age \
    --decrypt-identity ./backups/identity.txt

# 4. Restore database separately
owuicli restore-database --file ./backups/backup-*.zip.age \
    --decrypt-identity ./backups/identity.txt
```

## Configuration

### Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `OPEN_WEBUI_URL` | Open WebUI instance URL | ✅ |
| `OPEN_WEBUI_API_KEY` | API key for authentication | ✅ |
| `OWUI_ENCRYPTED_RECIPIENT` | Age public key for backup | ✅ (or use flag) |
| `OWUI_DECRYPT_IDENTITY` | Path to age identity file | ✅ (or use flag) |

### Example .env

```bash
export OPEN_WEBUI_URL="https://openwebui.example.com"
export OPEN_WEBUI_API_KEY="sk-xxxxxxxxxxxxxxxxxxxxxx"
export OWUI_ENCRYPTED_RECIPIENT="age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"
export OWUI_DECRYPT_IDENTITY="$HOME/.age/identity.txt"

# PostgreSQL database backup (optional)
export POSTGRES_URL="postgresql://user:password@localhost:5432/dbname"

# PostgreSQL binary paths (optional - for non-standard installations like Homebrew)
export PSQL_BINARY="/opt/homebrew/opt/libpq/bin/psql"
export PG_DUMP_BINARY="/opt/homebrew/opt/libpq/bin/pg_dump"
export PG_RESTORE_BINARY="/opt/homebrew/opt/libpq/bin/pg_restore"
```

## Encryption

All backups use [age](https://age-encryption.org/) encryption with X25519 public key cryptography.

### Team Setup

```bash
# Alice generates keys
age-keygen -o alice_identity.txt
ALICE_KEY=$(age-keygen -y alice_identity.txt)

# Bob generates keys
age-keygen -o bob_identity.txt
BOB_KEY=$(age-keygen -y bob_identity.txt)

# Create backup for both
owuicli backup --out team.zip \
    --encrypt-recipient $ALICE_KEY \
    --encrypt-recipient $BOB_KEY

# Either can restore
owuicli restore --file team.zip.age --decrypt-identity alice_identity.txt
```

### Server Command (owuiback)

The web server provides a dashboard interface for backup/restore operations:

#### serve

Start the web interface for backup/restore operations.

```bash
# Start web server (default: http://localhost:8080)
owuiback serve

# Custom port
owuiback serve --port 3000
```

## Docker

```bash
# Build image
docker build -t owuiback:latest .

# Run backup
docker run --rm \
  -e OPEN_WEBUI_URL="https://instance.com" \
  -e OPEN_WEBUI_API_KEY="sk-xxx" \
  -e OWUI_ENCRYPTED_RECIPIENT="age1..." \
  -v $(pwd)/backups:/backups \
  owuiback:latest backup --out /backups/backup.zip

# Web interface
docker run -p 8080:8080 \
  -e OPEN_WEBUI_URL="https://instance.com" \
  -e OPEN_WEBUI_API_KEY="sk-xxx" \
  owuiback:latest serve
```

## Development

```bash
# Install dependencies
task bootstrap

# Build
task build

# Run locally
task run -- backup --out test.zip

# Run tests
go test -v ./...
```

## Releases

The project uses GitHub Actions to automatically build cross-platform binaries when a tag is pushed.

### Creating a Release

1. **Tag the release:**
   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```

2. **Workflow builds:**
   - Linux (x64, ARM64)
   - macOS (x64, ARM64)
   - Windows (x64, ARM64)

3. **GitHub Release created with:**
   - Compressed binaries (`.tar.gz` for Unix, `.zip` for Windows)
   - SHA256 checksums
   - Auto-generated release notes
   - Version info embedded in binaries

### Testing a Release

```bash
# Create and push a test tag
git tag v0.0.1-test
git push origin v0.0.1-test

# Watch the workflow at:
# https://github.com/vosiander/open-webui-backup/actions

# Download and verify
wget https://github.com/vosiander/open-webui-backup/releases/download/v0.0.1-test/owuiback-v0.0.1-test-linux-amd64.tar.gz
tar -xzf owuiback-v0.0.1-test-linux-amd64.tar.gz
./owuiback-linux-amd64 --version

# Verify checksum
wget https://github.com/vosiander/open-webui-backup/releases/download/v0.0.1-test/checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

### Version Information

Binaries include embedded version information accessible via `--version` flag:
- Git tag version
- Git commit hash
- Build date
- Go version used

## Architecture

- **Go Backend** - CLI and web server
- **Vue 3 Frontend** - Modern reactive UI
- **Age Encryption** - Public key cryptography
- **WebSocket** - Real-time operation progress
- **Plugin System** - Extensible command architecture

## Migration from v1.0

v2.0 introduces breaking changes:

```bash
# OLD (v1.0)
./owuiback backup-all --dir ./backups
./owuiback restore-all --dir ./backups/backup.zip

# NEW (v2.0)
./owuiback backup --out ./backups/backup.zip
./owuiback restore --file ./backups/backup.zip.age
```

**Key Changes:**
- 12 commands → 3 commands (backup, restore, purge)
- Optional encryption → Mandatory encryption
- Passphrase mode removed (public key only)
- `--dir` flag → `--out`/`--file` flags

## License

See LICENSE file.

## Contributing

Contributions welcome! Fork, create feature branch, add tests, submit PR.


## Experimental opt-in modern knowledge bundles

The original `backup`, `restore`, web UI and library defaults retain the legacy
format/API behavior. They do not provide the modern graph guarantee. For a bounded
Open WebUI **0.12.2**, owned-private, local, root-only knowledge workflow, use the
separate `owuicli modern-knowledge` command. Backups still require age encryption;
restores create new objects and emit durable private plaintext partial/success
reports. This experimental proposal is **not real-backend accepted**; real indexing
and retrieval acceptance remain incomplete. See [scope, commands, report contract and compatibility](OPTIN_KNOWLEDGE_COMPATIBILITY.md).
