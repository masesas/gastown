Bead title: `Q-<epic>-<n>: <one-line question>`
Labels: `gt:question,gt:keep`
Type: not slingable (`gate`, or `question` if supported)

Description:

```
id: Q-<epic>-<n>
class: proceed | block
question: <one sentence, answerable without opening code>
context: <requirement/AC it relates to; why the Mayor cannot decide>
options:
  A: <option> — <consequence>
  B: <option> — <consequence>
assumed: <A|B|...>          # proceed only
affected: <bead>, <bead>    # updated as marks propagate
rework_cost: <number of affected beads, rough size>
```

Answer format expected from the human (one line per question):

```
Q-<epic>-<n>: <option or text>
```
