# Benchmarks

The [`bench/`](https://github.com/hoophq/alcatraz/tree/main/bench) directory
holds a reproducible speed comparison against
[Presidio](https://github.com/data-privacy-stack/presidio)'s Python analyzer.
Both engines read the same generated corpus (186 documents across a
{100 B, 1 KB, 10 KB, 1 MB} × {no PII, sparse, dense} matrix, every seeded
value passing its real checksum) and emit the same JSON schema, so results
merge into one table.

Representative single-threaded run, with Presidio configured to use its slim
tokenization-only NLP engine, the closest match to Alcatraz's pattern-only
core (its default spaCy-NER pipeline is slower still):

| Corpus group | Alcatraz ms/doc | Presidio ms/doc | Speedup |
|--------------|----------------:|----------------:|--------:|
| 100 B, no PII |           0.09 |             8.5 |   ~100x |
| 1 KB, no PII  |           0.80 |            15.2 |    ~19x |
| 10 KB, no PII |           8.0  |            84.3 |    ~11x |
| 10 KB, dense  |           8.7  |           116.1 |    ~13x |
| 1 MB, no PII  |          840   |         7,999   |    ~10x |
| 1 MB, dense   |        1,521   |       159,237   |   ~105x |

A parity check also diffs the detections of both engines on the same corpus.
On the shared entity types (credit cards, IBANs, SSNs, IPs, bank numbers) the
two agree span for span; they diverge where recognizer sets differ (e.g.
Presidio ships no Brazilian recognizers).

Numbers vary by machine; reproduce them with two commands per engine. See
[`bench/README.md`](https://github.com/hoophq/alcatraz/blob/main/bench/README.md)
for setup and methodology.
