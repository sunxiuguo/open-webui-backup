# Explicit modern knowledge opt-in (experimental draft proposal)

## Commands

Use an existing configured API key and existing age recipients/identity files.
This command never generates credentials, changes server configuration, or adds
permissions. It does not auto-include a database, even when POSTGRES_URL is set.

    owuicli modern-knowledge --help
    owuicli modern-knowledge backup --out private-kb.age --encrypt-recipient EXISTING_AGE_RECIPIENT
    owuicli modern-knowledge restore --file private-kb.age --decrypt-identity EXISTING_IDENTITY_FILE --report-dir EXISTING_DIRECTORY

Existing OWUI_ENCRYPTED_RECIPIENT and OWUI_DECRYPT_IDENTITY environment conventions
remain supported. The new command requires age encryption; there is no plaintext
backup switch. The output gets `.age` if needed and must not already exist. Output
mode is 0600. Plain ZIPs exist only transiently in fresh 0700 temporary directories
and are removed on normal success/error return. Forced termination can leave a
private temporary directory; secure filesystem/host policy is still required.

The report directory defaults beside the encrypted input, never inside temporary
decryption storage. It must already exist. Reports are intentionally plaintext
0600 JSON and contain source/target owner IDs, file/KB mappings, attempts, errors
and complete/incomplete status. Protect them accordingly. Inspect an incomplete
report before any retry: objects may remain, unknown create outcomes may have no
returned ID, and no rollback, deletion, retry, dedup across invocations or resume
is offered. A preflight failure can occur before report creation. An empty bundle
returns complete-empty without target HTTP or a report. See the emitted result.

The new CLI emits a JSON result line with status, actual report_path and structured
result (plus existing informational logs). Successful complete/complete-empty is
exit 0; validation, transport, verification, persistence and partial failures are
nonzero. Output paths and counts are never used to infer remote success. No CLI
flags allow mixed data, overwrite or incompatible server versions.

## Compatibility and library APIs

The upstream baseline is vosiander/open-webui-backup main
1bbfcdbce1dc4d35e81bb9111b800f99c2c795ee. Original backup.go and client.go are unchanged.
Restore adds only preflight rejection of new formats. Legacy/default backup,
selective/mixed/full flows, legacy ListKnowledge/ListFiles and embedded model KB
callchains use original logic. Legacy limitations remain; preserving a path does
not certify its old error handling or original-upload fidelity.

Explicit graph API: BackupModernKnowledge (directory), BackupModernKnowledgeFile
(path, optional unified wrapper), RestoreModernKnowledge,
RestoreModernKnowledgeWithReport, RestoreModernKnowledgeToReportDir. Library ZIP
writers are plaintext 0600, as before; callers must encrypt/protect them. New CLI
always age-encrypts. Graph format is knowledge manifest version 1; generated
owui.json also declares format modern-knowledge-v1. Older staged manifest-bearing
bundles remain recognizable. Legacy archives are not migrated implicitly.

Explicit standalone dual-file API: BackupRepresentationFiles,
BackupRepresentationFilesUnified, RestoreRepresentationFile,
RestoreRepresentationFilesUnified and RestoreFileWithResult. Modern pagination is
ListRepresentationFiles / ListModernKnowledge. These do not change global lists.
The dual-file helpers have no new CLI command in this bounded revision. They are
not full graph restore and never promise association restoration.

Ordinary restore rejects modern knowledge manifests, modern metadata declarations,
and representations.json markers (including unified subdirectories) before any
HTTP. Directory restore scans all ZIPs before an older model/user operation can
mutate. The same gate applies to direct legacy ZIP entrypoints and CLI default
restore. Arbitrary ZIPs stripped of every format marker cannot be identified as
modern; do not edit archives to bypass the guard.

## Scope and validation limits

Only exact source/target version 0.12.2; owned-private local root-only KBs with
explicit empty access_grants. Capture verifies pagination/count/drift, original A,
indexed B, metadata checksums and shared-file identity. Restore validates complete
input before HTTP, rejects empty B, creates only new resources, verifies A/B and
exact associations and persists partial progress. Readbacks do not prove vector
quality. Legacy/mixed modern bundles, ACLs, directories and unknown versions fail.

This new source has only synthetic HTTP/CLI tests, not true-backend validation.
Real indexing and retrieval acceptance have not been completed. No production
recovery or validated deployment is claimed; a synthetic transport round trip is
not a substitute for a real-backend round trip.

Tests compile and invoke the actual CLI against synthetic HTTP services, use the
published age v1.2.1 testdata/example_keys.txt identity (not a real credential), and
assert encryption, file modes, overwrite refusal, success/partial exit codes,
durable reports and default downgrade rejection. Existing synthetic archive/graph
failure and empty-B tests target the explicit APIs. Independent legacy/model
coverage remains required before any release decision.
