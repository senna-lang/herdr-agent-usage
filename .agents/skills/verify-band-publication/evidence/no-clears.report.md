# BandPublication

TLC reached 5 states and 5 steps. The chart draws 2 states and 2 arrows.
The chart does not draw 1 steps that stay in one chart state.

## QuiescentExclusive breaks
The shortest path has 4 steps:
0. Publishing: comfortable row
1. Critical: read the current sidebar: Publishing: comfortable row
2. Critical: update the comfortable band: Publishing: comfortable row
3. Critical: update the middle band: Publishing: comfortable row
4. Critical: update the critical band: Publishers finished: contradictory rows together

## Rules that hold
NoDeadlock
