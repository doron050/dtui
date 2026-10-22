# Deep-link rule bundles

The dtui extension is a **generic** deep-link engine — it ships with no
customer-specific rules. Each application provides its own rule bundle and users
import it on the extension's **Options → Deep-link rules** page (Import from URL
or Import from file).

A bundle is JSON: either a bare array of rules, or an object
`{ "name": "...", "description": "...", "rules": [ ... ] }`. See the rule schema
comment in [`../default-rules.js`](../default-rules.js) and this worked
example:

- [`astroshop.rules.json`](./astroshop.rules.json) — a `request`-source rule that
  reads the Dynatrace RUM beacon's `vi=` session id straight off the beacon URL
  and offers **two grouped RUM jumps** (so they collapse into one selectable
  launcher). Validated live against the astroshop demo site
  (`docs/dev/learnings.md` §1.15b).

## Where bundles should live

An application should host its bundle **in its own repository** (e.g.
`myapp/.../dtui-rules.json`) and share the raw URL. Users then paste that
URL into "Import from URL", so the bundle versions with the app and this
extension stays free of any customer specifics. `astroshop.rules.json` here is a
worked example; edit its `environment` URL to point at your own tenant.
