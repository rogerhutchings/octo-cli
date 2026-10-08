# Scratchcard schema investigation

Schema inspected read-only at `https://api.backend.octopus.energy/v1/graphql/` on 8 October 2026. No account status query or mutation was run.

## `scratchOctoplusScratchcard`

The mutation takes `input: ScratchOctoplusScratchcardInput!` and returns a nullable `ScratchOctoplusScratchcard`. Its input has two fields, both required and non-null:

| Field | Type |
| --- | --- |
| `accountNumber` | `String!` |
| `scratchcardSessionExternalReference` | `UUID!` |

The result object has one field: `scratchcard: Scratchcard!`. `Scratchcard` exposes:

| Field | Type |
| --- | --- |
| `externalReference` | `UUID!` |
| `accountNumber` | `String!` |
| `accountUserId` | `Int!` |
| `status` | `ScratchcardStatus!` |
| `offer` | `OctoplusOfferType` (nullable) |
| `prize` | `OctoplusScratchcardPrize` (nullable union) |

`OctoplusScratchcardPrize` has two possible types: `OctoplusOfferType` and `StampsAwardedType`. `OctoplusOfferType` represents offer details, including `slug`, `name`, `description` and nullable `pointsCost`. `StampsAwardedType` represents stamps awarded. The union means a prize selection must account for either concrete type.

## Session and status relationships

The read-only `octoplusActiveScratchcardData(accountNumber: String!)` query returns `OctoplusActiveScratchcardData!`, with nullable sibling fields `activeSession: Session` and `scratchcard: Scratchcard`. `Session` exposes `externalReference: UUID!`, `startsAt: DateTime!` and `endsAt: DateTime!`. The mutation input's `scratchcardSessionExternalReference` therefore identifies a session; introspection does not establish whether or how the returned card is associated with that session beyond this input relationship and the active-status response shape.

The `ScratchcardStatus` enum declares `DID_NOT_WIN`, `PRIZE_NOT_YET_CLAIMED`, `PRIZE_CLAIMED`, `PRIZE_REJECTED` and `PRIZE_NOT_CLAIMED_IN_TIME`. These names establish the schema's available status labels only. Introspection does not document their precise meanings, transition rules or timing.

## Limits and implementation constraints

Introspection does not establish whether a session is currently playable, how a card or prize is produced, whether mutations can be retried safely, or the status semantics beyond the enum labels. It does not establish the app-only participation rule; respect the published condition. Wait to observe a real Monday session through the read-only status query before considering implementation. No Go wrapper, executable mutation, play/claim command or account-changing operation was added or run.
