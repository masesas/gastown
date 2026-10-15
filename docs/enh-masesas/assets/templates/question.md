Bead title: `Q-<epic>-<n>: <one-line question>`
Labels: `gt:question,gt:keep`
Type: `decision`, child of the epic (`--parent <epic>`). Never dispatched; closing it means the question is answered.

Description:

```
id: Q-<epic>-<n>
class: proceed | block
question: <one sentence, answerable without opening code>
context: <requirement/AC it relates to; why the Mayor cannot decide>
options:
  A: <option> — <consequence>
  B: <option> — <consequence>
assumed: <A|B|...> — <why: consistency with existing code, reversibility, safest/most restrictive, superset>
                            # block: the safe default used only for work that may proceed (see directives § 4)
change_seam: <the single place the decision lives (constant, query param, role check, prop)> — <what changes for each other option>
affected: <bead>, <bead>    # every implementer and consumer, incl. the contract owner's bead when the seam is in the contract; updated as marks propagate
rework_cost: <number of affected beads, rough size, given the change_seam>
```

Every affected task gets the acceptance criterion: "the decision for Q-<id> lives only at its change_seam; switching options touches only that seam and its tests".

Answer format expected from the human (one line per question):

```
Q-<epic>-<n>: <option or text>
```
