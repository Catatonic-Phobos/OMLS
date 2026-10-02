# Project preferences (learned)

- 2026-10-02: Work lands on `main` by default for this experimental phase — no PR/merge gate unless asked. Cloud agents push commits directly to `main`.
- 2026-10-02: OMLS base = stock Linux userspace only for 0.1–1.0 — no new kernel, no Popcorn/Stramash runtime dependency, no Linux fork unless userspace later proves a hard limit.
- 2026-10-02: Prototype stack = `omls-agent` + fabric (gRPC/mTLS) + `omls-master` Resource Graph/scheduler; statistical learning (EWMA/regression) before ML models or local LLM.
- 2026-10-02: OMLS lives in a **new dedicated git repo**, fully separate from KEEP-Up (company stack; unrelated to this project). Never scaffold OMLS inside the KEEP-Up workspace.
- 2026-10-02: Initial lab topology = **Debian + Linux Mint + Windows WSL** nodes (heterogeneous real machines, not cloud-only VMs).
- 2026-10-02: GitHub repo = `https://github.com/Catatonic-Phobos/OMLS`. License = **MIT for now** as an experimental/test phase; revisit/formalize legal only if the project matures.
- 2026-10-02: This Project chat is currently bound to **KEEP-Up-Phobos/keepup-separated**; cloud push to `Catatonic-Phobos/OMLS` fails until work continues from a Project/agent rooted on the OMLS repo (Cursor App on Catatonic-Phobos with write).
