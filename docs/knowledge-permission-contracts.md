# Knowledge permission contract evidence

This implementation is a conservative compatibility prototype. It is not a
claim of general legacy-version support or complete recovery verification.

## Exact contract pin

The only allowed destination version is the exact `/api/version` response
`{"version":"0.6.18"}` from an official Open WebUI installation.

Official tag v0.6.18 resolves to
`5fbfe2bdcadf5f157926f6551891e4dc0802b9f3`:

- [Production version route](https://github.com/open-webui/open-webui/blob/5fbfe2bdcadf5f157926f6551891e4dc0802b9f3/backend/open_webui/main.py#L1680-L1684)
  is registered at top level, has no login dependency, and returns VERSION.
- [Version source](https://github.com/open-webui/open-webui/blob/5fbfe2bdcadf5f157926f6551891e4dc0802b9f3/backend/open_webui/env.py)
  obtains VERSION from package metadata; package.json is exactly 0.6.18.
- [KnowledgeForm](https://github.com/open-webui/open-webui/blob/5fbfe2bdcadf5f157926f6551891e4dc0802b9f3/backend/open_webui/models/knowledge.py#L95-L99)
  declares `access_control: Optional[dict] = None`.
- [Create route](https://github.com/open-webui/open-webui/blob/5fbfe2bdcadf5f157926f6551891e4dc0802b9f3/backend/open_webui/routers/knowledge.py#L141-L166)
  and [update route](https://github.com/open-webui/open-webui/blob/5fbfe2bdcadf5f157926f6551891e4dc0802b9f3/backend/open_webui/routers/knowledge.py#L285-L319)
  consume that form and forward it to the knowledge model.

The current v0.11.4 pin
`8bd8b4fac5e059578ac0c74b3c18d11139f88b7d` instead declares
[access_grants](https://github.com/open-webui/open-webui/blob/8bd8b4fac5e059578ac0c74b3c18d11139f88b7d/backend/open_webui/models/knowledge.py#L165-L168).
Passing access_control to that form silently discards it; renaming the field
would not establish principal identity equivalence.

## Acceptance matrix

| Source collection | Destination | Result |
| --- | --- | --- |
| Legacy only | Exact official 0.6.18 | Permission-contract gate permits existing restore behavior |
| Any access_grants, including null/empty/mixed | Any | Refused before archive API operations |
| Legacy only | 0.11.4 | Refused before archive API operations |
| Legacy only | Any other/unverified version | Refused, even if it might actually be compatible |
| Legacy only | Missing/invalid/unavailable version response | Refused |
| Legacy only | Custom suffix such as 0.6.18-custom | Refused |
| No collection in archive | Any | Knowledge preflight does not restrict unrelated resource operations |

Version declarations are trusted, not attested. An unofficial fork claiming
exactly 0.6.18 is outside the verified contract. Original principal IDs are
preserved, not remapped; operators must independently confirm their identity
and meaning on the destination. Effective access and full recovery are not
proved by this gate.

Destination compatibility failures remain identifiable through wrapped errors.
Restore orchestration stops on those failures instead of logging them and
continuing to file synchronization or other resource writes. Earlier writes,
including standalone model files or resources processed before a collection,
are not rolled back. Ordinary API error handling is unchanged, and an upgrade
after the last version check is still possible.

## Regression impact and extension requirements

The original backup README at commit
`1bbfcdbce1dc4d35e81bb9111b800f99c2c795ee` specifies **no Open WebUI supported
version range**. It broadly documents backup/restore without a version gate.
This prototype introduces a restriction: previously working servers other than
0.6.18 will now be refused. Compatibility of those versions has not been
established either way. Review that tradeoff before adoption; do not describe
this as preserving all existing legacy compatibility.

Add another exact version only after pinning its official commit and verifying
the production version endpoint, actual create/update route form contracts,
permission retention, negative controls and regression tests. Do not infer a
range from one release. Do not advise downgrading an installed server merely
to match this allowlist.

## Validation boundaries

Field-only Pydantic evaluation and AST checks can prove the above shape and
legacy-field retention without importing the app. They do not run the server,
database, authentication, authorization, embedding or restoration. The Go tests
exercise synthetic JSON, ZIPs and an in-memory HTTP transport; they require
separate execution and are not substitutes for controlled end-to-end testing.
