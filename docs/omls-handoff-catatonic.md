# Handoff — continuar OMLS no repo certo

Cole este arquivo (ou o resumo abaixo) num **Project / Cloud Agent aberto em** https://github.com/Catatonic-Phobos/OMLS — **não** em KEEP-Up.

## Por que migrar

O Project anterior estava amarrado a `KEEP-Up-Phobos/keepup-separated`. O destino do código é `Catatonic-Phobos/OMLS`. Cloud agents sem write na conta **Catatonic-Phobos** recebem `403` / `cursor[bot]` denied.

## Decisões já fechadas

1. Sem kernel novo; Popcorn/Stramash = inspiração apenas; sem fork Linux em 0.1–1.0.
2. Stack: Linux stock userspace — `omls agent` + fabric (gRPC/mTLS) + `omls master`.
3. Aprendizado: stats/EWMA antes de ML/LLM.
4. Repo: https://github.com/Catatonic-Phobos/OMLS
5. Licença: **MIT** provisória (fase teste).
6. Lab: Debian + Mint + WSL (WSL degrada telemetria/PCI sem crash).
7. KEEP-Up = empresa, **zero** relação com este código.

## Docs de referência (Project store anterior)

Se o store antigo ainda estiver acessível neste ambiente:

- Vision: `/cursor/stores/bc-5367ae95-72d8-48e9-ad4b-90c43915c273/docs/omls-vision.md`
- Plan 0.1→0.3: `/cursor/stores/bc-5367ae95-72d8-48e9-ad4b-90c43915c273/docs/omls-plan-0.1-0.3.md`
- Preferences: `/cursor/stores/bc-5367ae95-72d8-48e9-ad4b-90c43915c273/preferences.md`

Artifacts do scaffold 0.1 (já implementado e testado localmente no agent antigo):

- `/cursor/stores/bc-5367ae95-72d8-48e9-ad4b-90c43915c273/artifacts/omls-0.1-discover.bundle`
- `/cursor/stores/bc-5367ae95-72d8-48e9-ad4b-90c43915c273/artifacts/omls-0.1-discover.patch`
- `/cursor/stores/bc-5367ae95-72d8-48e9-ad4b-90c43915c273/artifacts/omls-0.1-machine-profile.yaml`
- `/cursor/stores/bc-5367ae95-72d8-48e9-ad4b-90c43915c273/artifacts/omls-0.1-go-test.log`

Branch local que não pôde ser publicada: `cursor/omls-0.1-discover-2056` @ `cec0ffc`.

## Prompt para o agent no repo OMLS

```text
Continue OMLS 0.1 no repo atual (Catatonic-Phobos/OMLS).

Decisões: Linux stock userspace only; Go CLI `omls agent discover`; MIT; sem KEEP-Up; sem kernel/Popcorn.

1. Verifique write neste repo (push deve funcionar aqui).
2. Aplique o scaffold 0.1 a partir do bundle/patch se disponíveis no store:
   - omls-0.1-discover.bundle → branch cursor/omls-0.1-discover-2056
   - ou omls-0.1-discover.patch
   Se artifacts não estiverem acessíveis, reimplemente 0.1 conforme o plan:
   discover (/sys,/proc,PCI,USB,hwmon,CPUFreq,powercap), RDL v0 YAML/JSON,
   WSL/virt tagging, graceful degradation, tests, README, LICENSE MIT.
3. Push da branch e abra draft PR para main.
4. Não comece 0.2 até o PR 0.1 existir.

Aceite 0.1: `go test ./...` + `omls agent discover --out machine-profile.yaml` gera RDL válido.
```

## Depois do PR 0.1

Seguir [OMLS Plan 0.1→0.3](./omls-plan-0.1-0.3.md):

- **0.2** fabric gRPC + mTLS + register/graph
- **0.3** scheduler + demo workload + EWMA

## Checklist humano (1 minuto)

- [ ] Cursor aberto no repo **Catatonic-Phobos/OMLS**
- [ ] GitHub App Cursor instalado na conta **Catatonic-Phobos** com write em OMLS
- [ ] Novo Project/agent nesse repo (não no KEEP-Up)
- [ ] Colar o prompt acima + anexar/plan links se o store carregar
