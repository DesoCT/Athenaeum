# Non-functional measurements

Evidence for requirements N1, N2, and N3, and for acceptance test F4. Spec 08
makes measured numbers the Phase 3 exit gate, so these are recorded rather than
estimated.

Reproduce with:

```bash
make test-scale                                   # generates the corpus, measures N1-N3
./bin/athenaeum serve <fixture>/athenaeum.toml --port 7971 --no-open
cd web && ATHENAEUM_SCALE_URL="<bootstrap URL>" npx playwright test e2e/scale.spec.ts
```

## Method

The corpus is generated, not committed: 5,000 documents totalling 2 GB do not
belong in a repository. `test/scale/fixture.go` writes it deterministically from
a fixed seed, so a rerun measures the same corpus. Documents carry headings,
paragraphs, fenced code, lists, relative links, and — every twentieth file —
front matter with tags, spread over a shallow directory tree rather than one
flat directory of 5,000 entries.

The fixture must live on a real filesystem. `ATHENAEUM_SCALE_DIR` pointing at a
tmpfs would measure RAM rather than the disk a workspace actually lives on.

## Hardware

| | |
|---|---|
| CPU | Intel Core i7-1195G7, 8 threads |
| Memory | 30 GB |
| Storage | ext4 on LVM, SATA SSD |
| OS | Linux 7.0.0 |
| Go | 1.26.5, `CGO_ENABLED=0` |

A contemporary developer laptop, which is the machine N1 names.

## Corpora

Two corpora, because one number would mislead. N3 states a *ceiling* — "at
least 5,000 Markdown files, 2 GB total" — and a corpus at that ceiling averages
400 KB per document, which is far larger than Markdown usually runs. The
typical corpus holds the same 5,000 documents at a realistic 4 KB each.

| | Typical | N3 ceiling |
|---|---|---|
| Documents | 5,000 | 5,000 |
| Total content | 20 MB | 2.00 GB |
| Mean document | 4 KB | 400 KB |
| Index size | 31 MB (153%) | 2.64 GB (132%) |

The index is consistently larger than the corpus because FTS5 stores both the
column text and its inverted index. It is a disposable cache under the OS cache
directory (D-014), so the cost is disk, never durability.

## N1 — startup

> For a warm cache and ordinary workspace, the local server SHOULD become ready
> within two seconds.

Measured against the release binary, from process launch to `GET
/api/v1/health` returning 200.

| Workspace | Banner | Ready | Index confirmed current |
|---|---|---|---|
| Repository's own (19 documents) | 30 ms | 44 ms | 53 ms |
| 5,000 documents / 20 MB | 54 ms | **68 ms** | 77 ms |
| 5,000 documents / 2.00 GB | 65 ms | **79 ms** | 88 ms |

Well inside the two-second target, and effectively independent of corpus size.
Two design decisions produce that:

- indexing starts after the listener is open and never blocks it;
- a warm start compares stored size and modification time against the workspace
  and opens no file at all when nothing has changed, so confirming 5,000
  unchanged documents is one SQL query and a map comparison.

Startup on a **cold** cache is the same, because the build runs behind the
listener: with an empty cache over the 2 GB corpus the server answered health
at 82 ms while 4,744 documents were still queued.

## N2 — responsiveness

> UI interactions unrelated to indexing MUST remain responsive while indexing or
> Git commands run.

Queries issued continuously against the 2 GB corpus while a full rebuild ran
(status `rebuilding`, thousands of documents queued):

| Corpus | Queries sampled | p50 | p95 | Max |
|---|---|---|---|---|
| 20 MB | 180 | 16.7 ms | 19.6 ms | 33.4 ms |
| 2.00 GB | 80 | 376 ms | 423 ms | 441 ms |

Through the HTTP API during an active 2 GB rebuild, a rare-token search returned
correct results in 8–11 ms, and the index served all 5,000 previously indexed
documents throughout — a rebuild refreshes rows in place and never empties the
projection.

In the browser at the 2 GB scale, the worst quick-open round trip measured
during a rebuild was **57 ms**.

WAL mode is what makes this hold: readers proceed while the single writer holds
its transaction, so a query never queues behind indexing.

## N3 — scale

> v0.1 MUST support at least 5,000 Markdown files and 2 GB total content.

Both corpora index and serve completely.

| | Typical (20 MB) | Ceiling (2.00 GB) |
|---|---|---|
| Workspace enumeration | 22 ms | 24 ms |
| Full index build | 1.07 s (4,678 docs/s) | 61.8 s (81 docs/s) |
| Warm re-confirmation | 51 ms | 51 ms |

Query latency on a settled index, 20 samples each:

| Query | Typical p50 | Typical p95 | Ceiling p50 | Ceiling p95 |
|---|---|---|---|---|
| Single common term | 10.5 ms | 29.0 ms | 244 ms | 249 ms |
| Two common terms | 13.3 ms | 13.7 ms | 338 ms | 344 ms |
| Rare exact token | 161 µs | 445 µs | 1.4 ms | 1.7 ms |
| Quoted phrase | 148 µs | 258 µs | 139 µs | 251 µs |
| Prefix, mid-typing | 10.5 ms | 11.0 ms | 242 ms | 250 ms |

### The common-term result at the ceiling

At the 2 GB ceiling, a query for a word appearing in every document costs about
250 ms. That cost is FTS5's ranking, not Athenaeum's: profiling shows a query
returning a *single* result already costs 205 ms, so it is not the per-result
work of reading files and building snippets. `bm25()` must score every matching
row to order them, and a term present in all 5,000 documents of a 2 GB corpus
has a very long posting list.

It is stated rather than hidden because it is the honest shape of the feature:
common-word searches on an extreme corpus are noticeably slower than everything
else, while a rare term — what a user is usually hunting for — stays at 1.4 ms.
Reducing it further would mean either approximate ranking or a secondary
structure, both of which exceed what R7 asks for.

An earlier implementation was far worse. FTS5's `snippet()` re-tokenises a whole
column to choose its window, costing about 6 ms per row on a 100 KB document,
and it was being called twice per row over a scan bound rather than the returned
page. Building snippets in Go from the file already read for match location took
a common-term query on a 200-document corpus from 846 ms to 12 ms.

## F4 — acceptance

> A generated corpus of 5,000 documents remains navigable and searchable without
> blocking primary UI interaction.

Browser tests against the 2 GB corpus (`web/e2e/scale.spec.ts`), all passing:

| Scenario | Result |
|---|---|
| Workspace opens and reports 5,000 documents | pass |
| File tree expands a directory | 386 ms |
| Quick open finds one document by name | 228 ms |
| Search returns a located, highlighted result | 390 ms |
| Opening a result lands on the matched line | 739 ms |
| Quick open during a full rebuild | 57 ms worst case |

## R7 — searchability latency

> Changes SHOULD become searchable within two seconds.

Measured end to end against the repository's own workspace: an external edit
became searchable through the HTTP API in **289 ms**. The watcher's coalescing
window is 250 ms, so that is close to the floor this design allows.
