# Fob

A local OpenAI-compatible proxy that turns Claude, Codex, Grok, and Cursor subscriptions into keys other tools can call.

## Language

**Credential**:
An OAuth login for one upstream provider, stored encrypted in SQLite.
_Avoid_: account, auth file, token

**LocalKey**:
A `sk-fob-…` secret the operator hands to a tool. Fob hashes it; the tool sends it as Bearer.
_Avoid_: API key, user, token

**Executor**:
The module that speaks one upstream provider’s wire format.
_Avoid_: client, adapter, driver

**Vault**:
The SQLite store of Credentials, encrypted with `JWT_SECRET`.
_Avoid_: auth dir, keychain

**Meter**:
Append-only usage events plus API-equivalent dollar estimates from models.dev list prices.
_Avoid_: billing, invoice, quota

**Sub**:
Live remaining windows on a Credential’s subscription, fetched on demand from the provider.
_Avoid_: quota, billing, invoice

**Setting**:
A panel-editable key/value in SQLite. Not boot geometry.
_Avoid_: config, env
