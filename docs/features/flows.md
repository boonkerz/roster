# Flows (flow designer)

Recurring changes on many servers – swapping an image in Compose files, changing a port from
`8000` to `80`, writing a version from an API into a `.env` – are built as a **flow** of blocks
connected by lines. The agent itself runs it (no `sed`, `bash` or extra tools), with a
**preview**, **backups** and **rollback**.

![Flow designer with blocks and connections](../screenshots/flows-editor.png){ .shadow }

## Editor

Under **Flows** create a new flow or start **from a template**. Blocks are on the left, the canvas
in the middle, the properties of the selected block on the right. Every block has an input at the
top and two outputs at the bottom:

- **ok** (green) – continue when the block succeeded,
- **Error** (red) – branch for the failure case, e.g. to *Roll back*.

A flow has exactly one start block (no incoming line) and must not contain cycles. If a block's
error output is **not connected**, the flow stops there and **automatically** restores every file
changed so far.

**Parameters** (e.g. `FROM`, `TO`) are asked by the wizard before starting; inside the flow they
are `{{param.FROM}}`.

## Blocks

| Block | What it does | In the preview |
|---|---|---|
| **Find files** | Paths/globs (one per line, optional "contains …") **or** the Compose files of all containers with an image (via the Compose labels; no tag = all tags; plus the `.env` if the image is there). Its own backups (`*.roster-bak-*`) are never matched. No matches = device not affected, the flow ends without error. | runs |
| **Replace text** | Literal or regular expression (`$1` …), optionally only in lines containing a text (e.g. `image:`). A backup `<file>.roster-bak-<run>` is written per file first. Without matches the flow ends here without error (device already switched), unless "Continue even if nothing was replaced" is set. | shows the diff, changes nothing |
| **Read value** | Reads a value from YAML/JSON (path like `services.php-fpm.image`, `"a.b"` for dots in names, number = list index) or a key from a `.env`. *Part* extracts the tag (`…:2.4.0` → `2.4.0`, no tag `latest`) or the repository from an image reference. Result: `{{steps.ID.value}}`. | runs |
| **Edit YAML** | Edits YAML precisely, comments, quotes and indentation stay: `set path = value`, `append path = item` (only if new), `ensure path = value` (only if missing), `delete path [= list item]`. Missing parts are created. `{a,b,c}` in the path applies to several entries, `{{value.repo}}`/`{{value.tag}}`/`{{value}}` in the value are the current value. Backup and diff as with *Replace text*; without changes the flow ends. | shows the diff |
| **Docker Compose** | `pull`, `up -d`, `restart`, `down` – for the projects found (affected services only) or a directory; `docker compose` with fallback to `docker-compose`. | shows the commands |
| **Command / script** | Own command or a library script; exit ≠ 0 = failure. | not executed |
| **Query API** | HTTP request with headers/body; a JSON path yields a value (`{{steps.api.value}}`). | GET/HEAD only |
| **Check** | Container running (without a name: the affected Compose services), HTTP status, TCP port, file contains text, compare value – also as a version (`2.10.0 > 2.9.1`, `v` prefix ignored, `-rc` lower than the final release) – with retries. *Not met: end the flow without error* turns it into a condition ("otherwise do nothing"). | comparisons and "file contains" run, the rest is skipped |
| **Roll back** | Restores every file changed in this run, optionally followed by `up -d`. | skipped |
| **Wait** | Waits up to 10 minutes. | skipped |

**Placeholders:** `{{param.NAME}}`, `{{steps.ID.value}}`, `{{steps.ID.out}}` (command output),
`{{steps.ID.json.path.0.field}}`, `{{steps.ID.files}}` and the device fields `{{agent.field}}`,
`{{client.field}}`, `{{site.field}}`.

## Running: preview → test server → rollout

![Wizard with preview and diff](../screenshots/flows-run.png){ .shadow }

1. Choose **parameters and a preview server**, *Start preview*: that server's agent runs the flow as
   a dry run and shows per block what would happen – including the diff of every file.
2. **Run on test server**: the same server, this time for real (with backups).
3. **Roll out** to a company, a site, a tag, all devices or – from the Containers tab – the affected
   devices. The table shows the state per device; devices without matches appear as *not affected*.

**Flows → Runs** lists earlier runs with their results; single runs can be deleted. **Copy** (in the
list or in the editor) creates a flow as the starting point for a variant – in the editor including
unsaved changes.

## Sharing flows (JSON)

Flows can be shared as a file in the **roster-flow** exchange format (version 1): **Export** in the
list or in the editor downloads `<name>.roster-flow.json`, **Import** in the list accepts such a file
(upload or paste), validates it (*Check* changes nothing) and creates a new flow – one with the same
name stays unchanged and the import is called "Name (Import)".

- **Library scripts** are embedded on export (`script` + `shell`) because their IDs do not exist on
  other servers; a document with `script_id` is rejected on import.
- **Positions** (`x`, `y`) are optional; without them the editor stacks the blocks vertically.
- The **JSON Schema** is at
  [roster-rmm.dev/schemas/roster-flow-1.schema.json](https://roster-rmm.dev/schemas/roster-flow-1.schema.json).
  With `"$schema"` in the file, editors like VS Code validate and complete blocks and settings while
  you type.
- Via API: `GET /api/v1/flows/{id}/export`, `POST /api/v1/flows/import` (with `?dry_run=1` to only
  validate).

```json
{
  "$schema": "https://roster-rmm.dev/schemas/roster-flow-1.schema.json",
  "format": "roster-flow",
  "version": 1,
  "name": "Switch port 8000 to 80",
  "params": [{ "name": "DIR", "label": "Compose directory", "required": true }],
  "nodes": [
    { "id": "find", "type": "find_files", "config": { "mode": "paths", "paths": "{{param.DIR}}/docker-compose.yml" } },
    { "id": "repl", "type": "replace", "config": { "search": "\"8000:8000\"", "replace": "\"80:8000\"" } },
    { "id": "up", "type": "compose", "config": { "action": "up", "all_services": true } },
    { "id": "rb", "type": "rollback", "config": { "compose_up": true } }
  ],
  "edges": [
    { "from": "find", "port": "ok", "to": "repl" },
    { "from": "repl", "port": "ok", "to": "up" },
    { "from": "up", "port": "fail", "to": "rb" }
  ]
}
```

## Templates

- **Compose: raise version if older** – reads the image tag of a service (e.g. `php-fpm`), checks "older than 2.5.0", sets the images of `web`, `php-fpm`, `cron` to 2.5.0 (repository stays), adds a mount and a named volume, `pull`, `up -d`, checks; otherwise *not affected*. The core is this *Edit YAML* block:

    ```
    set    services.{web,php-fpm,cron}.image = {{value.repo}}:2.5.0
    append services.{php-fpm,cron}.volumes = shop_media:/var/www/media
    ensure volumes.shop_media = {}
    ```

- **Replace image in Compose** – find Compose files with the old image, replace it in the `image:`
  line, `pull`, `up -d`, wait briefly, check; on failure roll back and bring the previous state up.
  Under **Security → Containers**, *Replace image …* opens this template with the image and the
  affected devices prefilled (e.g. `jrcs/letsencrypt-nginx-proxy-companion` →
  `nginxproxy/acme-companion`).
- **Replace value in file and restart services** – e.g. port `8000` → `80`, restart the project,
  check the port.
- **Query API and write value** – read a value by JSON path and write it into a file via regular
  expression.

!!! note "Permissions"
    Editing flows requires the *Script library* permission, running them *Operate devices*. Every
    start is in the audit log. The agent runs flows with its own privileges (root or SYSTEM) –
    backups stay next to the files.
