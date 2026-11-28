# BandPublication

TLC reached 41 states and 63 steps. The chart draws 7 states and 12 arrows.
The chart does not draw 38 steps that stay in one chart state.

## QuiescentExclusive breaks
The shortest path has 8 steps:
0. Publishing: empty sidebar
1. Critical: read the current sidebar: Publishing: empty sidebar
2. Critical: update the comfortable band: Publishing: empty sidebar
3. Critical: update the middle band: Publishing: empty sidebar
4. Comfortable: read the current sidebar: Publishing: empty sidebar
5. Critical: update the critical band: Publishing: critical row
6. Comfortable: update the comfortable band: Publishing: contradictory rows together
7. Comfortable: update the middle band: Publishing: contradictory rows together
8. Comfortable: update the critical band: Publishers finished: contradictory rows together

## Rules that hold
NoDeadlock
