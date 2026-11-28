----------------------- MODULE BandPublication -----------------------
EXTENDS Naturals, FiniteSets, Sequences

CONSTANTS Writers, SeedHigh, ClearInactive
ASSUME /\ Writers \subseteq {"Comfortable", "Critical"}
       /\ Writers # {}
       /\ SeedHigh \in BOOLEAN
       /\ ClearInactive \in BOOLEAN

Bands == <<"limit_high", "limit_mid", "limit_low">>
Names == {Bands[i] : i \in 1..Len(Bands)}
Empty == [name \in Names |-> ""]
InitialTokens == [Empty EXCEPT !["limit_high"] = IF SeedHigh THEN "5h 94%" ELSE ""]
Active(w) == IF w = "Comfortable" THEN "limit_high" ELSE "limit_low"
Text(w) == IF w = "Comfortable" THEN "5h 80%" ELSE "5h 20%"
Desired(w, name) == IF name = Active(w) THEN Text(w) ELSE ""

VARIABLES tokens, snapshot, pc
vars == <<tokens, snapshot, pc>>

Init == /\ tokens = InitialTokens
        /\ snapshot = [w \in Writers |-> Empty]
        /\ pc = [w \in Writers |-> 0]

\* GetPaneInfo returns one immutable map before a publisher starts its writes.
ReadSnapshot(w) ==
    /\ pc[w] = 0
    /\ snapshot' = [snapshot EXCEPT ![w] = tokens]
    /\ pc' = [pc EXCEPT ![w] = 1]
    /\ UNCHANGED tokens

\* A successful per-token CLI operation is atomic; the family is not atomic.
\* The skip decision depends only on the frozen map, not on live tokens.
Publish(w, i) ==
    LET name == Bands[i]
        value == Desired(w, name)
        shouldWrite == snapshot[w][name] # value
        allowed == ClearInactive \/ value # ""
    IN /\ pc[w] = i
       /\ tokens' = IF shouldWrite /\ allowed
                     THEN [tokens EXCEPT ![name] = value]
                     ELSE tokens
       /\ pc' = [pc EXCEPT ![w] = i + 1]
       /\ UNCHANGED snapshot

WriteHigh(w) == Publish(w, 1)
WriteMid(w) == Publish(w, 2)
WriteLow(w) == Publish(w, 3)
Finished == \A w \in Writers : pc[w] = 4
Done == /\ Finished
        /\ UNCHANGED vars
Next == \/ \E w \in Writers : ReadSnapshot(w)
        \/ \E w \in Writers : WriteHigh(w)
        \/ \E w \in Writers : WriteMid(w)
        \/ \E w \in Writers : WriteLow(w)
        \/ Done

\* Temporary mixed states are not findings: check only after all writers finish.
QuiescentExclusive ==
    Finished =>
        /\ Cardinality({name \in Names : tokens[name] # ""}) = 1
        /\ \A name \in Names :
               tokens[name] # "" =>
                   \E w \in Writers : name = Active(w) /\ tokens[name] = Text(w)
Spec == Init /\ [][Next]_vars
======================================================================
