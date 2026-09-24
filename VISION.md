# Vision

router-axi exists so that an agent or a technical owner can operate a supported home router from a predictable command line without scraping a browser UI.
It serves local-network operators who need trustworthy inspection and deliberate automation.
It owns exactly one thing: an agent-ergonomic CLI over supported router capabilities.

## Agent-facing interface

Every command is non-interactive and scriptable.
Commands return compact structured text by default and JSON when explicitly requested.
Lists expose only the fields needed for identification and decision-making by default.
Large bodies are truncated with their omitted size and a clear full-content escape hatch.
Empty results state that zero results were found.
Commands include relevant aggregate state when it removes a follow-up query.
Each successful command suggests the next valid operation when one is useful.
Help is concise, consistent, and available at every command level.

## Read before change

Read-only inspection is the default surface.
A state-changing command shows its intended effect before it executes.
Disruptive changes require an explicit confirmation flag and never rely on a prompt.
Mutations are idempotent where the underlying router capability permits it.
Wi-Fi changes verify the requested state; router reboot is explicitly not idempotent.
Configuration export is a read of the router, not a mutation: it writes only to an
explicitly named destination through an atomic owner-only file, never overwrites
an existing file without an explicit flag, and its export passphrase comes only
from a dedicated environment variable, is never reused from login credentials,
and never appears in output. The one-time download address and export contents
are never printed; downloads use HTTPS, refuse redirects and plaintext, and fail
closed on untrusted certificates.
Reboot previews identify the selected endpoint, the interruption of all local services,
and the exact confirmed command. A confirmed reboot sends the documented
DeviceConfig:Reboot action once, reports acknowledgement rather than recovery,
and never retries or polls after initiation. A lost response is an uncertain
outcome, not success or permission to repeat. The operator waits for recovery,
reconnects if necessary, and checks the router manually; no recovery deadline
or rollback is promised.
Commands report a machine-readable result and a non-zero exit code on failure.
Secrets never appear in normal output, error messages, or debug logs.

## Honest capability boundaries

The CLI reports unsupported router models, firmware capabilities, and authentication requirements explicitly.
It does not simulate success when the router rejected or could not confirm an operation.
It preserves protocol details behind a stable command contract without promising that undocumented router endpoints are permanent.
Compatibility is earned through tested behavior, not inferred from model names.

## Focus

router-axi is a local router-operations CLI.
It is not a router web interface, a cloud service, a home-automation platform, or a general network scanner.
It does not collect telemetry or require a hosted account to operate a local router.
It does not add an integration until its operational value and safety boundary are clear.

## Change test

A change aligns when it makes a supported router action more observable, safer, more deterministic, or less costly for an agent to perform.
A change should be resisted when it replaces explicit local control with hidden automation, broadens into unrelated network management, or increases default output without eliminating a decision.
