<!-- Defines the mandatory architecture and coding rules for this repository. -->

# Project Instructions

## Scope

These instructions apply to the entire repository. Treat the rules below as architectural constraints, not preferences.

## Provider Extensibility

- The set of providers is open-ended. Assume that providers will be added, removed, or changed.
- Provider-neutral code MUST NOT depend on the current provider count or duplicate the provider list outside the canonical registry.
- A provider implementation MUST conform to the shared provider contracts and register through the same mechanism as every other provider.
- Provider-specific code MAY translate authentication, session, transcript, model, or API formats into shared domain types. It MUST NOT redefine shared domain behavior.

## Provider Symmetry

- Shared code MUST interact with every provider through the same provider-neutral contracts.
- Shared code MUST operate on common domain types rather than branch on provider names, model names, or vendor-specific types.
- No provider may receive a privileged path through shared layers. A change of provider must change the adapter and its data, not the surrounding architecture.
- If a requirement applies across providers, add it to the shared contract. If it exists only because of an external provider format or capability, contain it within that provider's adapter.
- Provider adapters may differ internally only where their external systems differ. Those differences MUST end at the adapter boundary.

## Directory and Package Boundaries

- Treat each directory as an architectural module and preserve the repository's dependency direction.
- This repository is written in Go. A package's exported identifiers are its public API; unexported identifiers are implementation details. Consumers MUST depend on the package API rather than another package's implementation details.
- Provider packages MUST NOT import one another. Behavior shared by multiple providers belongs in a provider-neutral package at the appropriate dependency layer.
- The Go `internal` visibility boundary and every lint rule configured in `.golangci.yml` are mandatory.
- Do not bypass a boundary with deep imports, duplicate implementations, or convenience dependencies. If the desired dependency violates the graph, move the shared abstraction to the correct layer instead.

## Exhaustive Constraints for Evolving Definitions

- Definitions expected to evolve, including providers and other registries, MUST have one canonical source of truth.
- Dependent dispatch tables, mappings, and registries SHOULD be derived from that source where practical.
- When derivation is not practical, add a compile-time or test-time exhaustiveness constraint so that adding or changing a definition exposes every missing dependent update.
- In Go, use shared interfaces, compile-time interface conformance checks, typed registries, and exhaustive contract tests as appropriate.
- Do not hide missing cases behind permissive defaults. A newly added definition must fail compilation or a focused contract test until all required behavior is implemented.

## Human-Readable Code

- Keep functions and modules focused, and keep statements within a function at a consistent level of abstraction.
- Prefer explicit, boring control flow over clever compression or provider-specific shortcuts.
- Split large or complex operations into named steps whose names explain the domain behavior.
- Add concise inline comments to complex processing. Translate the intent, ordering constraints, and non-obvious invariants into natural language; do not merely restate the syntax.
- Use domain names rather than vendor names in shared code unless the value genuinely represents a vendor-specific concept.

## Change Checklist

For any provider, model, registry, or shared provider behavior change:

1. Confirm that every provider still implements the same shared boundary.
2. Confirm that shared code contains no new provider-name or model-name branch.
3. Confirm that provider-specific differences remain inside their adapters.
4. Confirm that package boundaries and dependency direction remain intact.
5. Confirm that exhaustive constraints detect omitted dependent updates.
6. Add or update focused contract tests for the changed behavior.
7. Run the relevant package tests, then the repository checks required by `CONTRIBUTING.md`.
