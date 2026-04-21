# Mihomo Config Corpus

This corpus is used by automated prebuild tests to harden runtime config handling.

## Structure

- `valid/` — configs that should parse and pass Sloth runtime pipeline normalization.
- `invalid/` — malformed YAML samples that must fail parsing.

## Notes

- Files are intentionally synthetic and broad (DNS, TUN, providers, groups, mixed proxy types).
- They are not tied to one subscription provider.
- Add new edge cases here before touching parser/merge/pipeline logic.
