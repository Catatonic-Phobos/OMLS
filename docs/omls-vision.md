# OMLS — Operational Machine Learning System

## 1. Visão geral

OMLS significa **Operational Machine Learning System**.

A proposta não é criar apenas um novo Operating System, nem apenas aplicar Machine Learning ao sistema operacional.

A ideia central é:

> **Um OMLS aprende como operar a própria máquina.**

O sistema descobre hardware, identifica capacidades, modela dependências físicas e lógicas, coordena recursos heterogêneos e aprende como aquele conjunto específico de hardware se comporta ao longo do tempo.

O objetivo é transformar computadores, placas, aceleradores, dispositivos, equipamentos standalone e máquinas conectadas em uma **máquina lógica dinâmica e modular**.

---

## 2. Princípio fundamental

Em sistemas tradicionais, o computador é definido principalmente por uma topologia física relativamente estática:

```text
CPU
RAM
GPU
Storage
Peripherals
```

No OMLS, a máquina é definida pelas **capacidades disponíveis**.

A estrutura fundamental é:

```text
DEVICE
   ↓
RESOURCE
   ↓
FUNCTION
   ↓
BEHAVIOR
```

### DEVICE

A peça física ou lógica.

Exemplos:

```text
GPU
CPU
FPGA
HDD
placa-mãe
computador remoto
microscópio
sensor
```

### RESOURCE

Capacidade oferecida pelo dispositivo.

Exemplos:

```text
compute
graphics
memory
storage
video_encode
network
display
signal_processing
```

### FUNCTION

O que o sistema deseja executar.

Exemplos:

```text
render
compile
encode_video
capture_image
run_inference
```

### BEHAVIOR

Como aquele recurso realmente se comporta naquela máquina específica.

Exemplos:

```text
consumo
latência
temperatura
estabilidade
eficiência
limites
degradação
tempo de resposta
```

---

## 3. A máquina descobre a própria topologia

O kernel/control plane não deve assumir completamente a configuração da máquina.

Durante boot ou conexão de novos dispositivos:

```text
DISCOVER
↓
DESCRIBE
↓
REGISTER
↓
MODEL
↓
RUN
```

Exemplo:

```text
DEVICE FOUND

type:
graphics

capabilities:
compute
render
video_encode

memory:
8 GB

transport:
PCIe
```

Essas informações entram em um **Resource Graph**.

---

## 4. Resource Graph

A máquina deve ser representada como um grafo de recursos e dependências.

Exemplo:

```text
MASTER
│
├── CPU
├── GPU0
├── GPU1
├── FPGA
├── RAM
├── Storage
└── Network
```

Mas também pode representar fluxos:

```text
GPU0
 ↓
GPU1
 ↓
GPU2
 ↓
OUTPUT
```

ou execução paralela:

```text
       ┌→ GPU0 ─┐
MASTER ├→ GPU1 ─┼→ RESULT
       └→ GPU2 ─┘
```

Ou topologias híbridas.

O sistema passa a enxergar a máquina como um **patchbay computacional**.

---

## 5. Master / Coordinator

O processador principal não precisa executar todo o trabalho.

Seu papel pode ser principalmente:

```text
descobrir recursos
manter o Resource Graph
distribuir funções
observar comportamento
controlar energia
gerenciar transporte
aplicar políticas
```

Ou seja:

> O master não precisa fazer tudo. Ele precisa saber quem consegue fazer cada coisa.

---

## 6. Resource Envelope

Inspirado conceitualmente em envelopes de sintetizadores.

Em vez de tratar recursos apenas como:

```text
OFF
100%
OFF
```

um recurso pode receber um comportamento operacional.

Exemplo:

```text
ATTACK
PEAK
SUSTAIN
RELEASE
```

Aplicado a uma GPU:

```text
attack = rápido
peak = 90%
sustain = 40%
release = 2s
```

Esse conceito é chamado de:

### Resource Envelope

O envelope pode incluir:

```text
performance
power
temperature
priority
latency
duration
```

A função não especifica apenas:

> execute isto.

Ela também pode especificar:

> execute isto desta maneira.

---

## 7. Power Fabric

Energia também deve ser tratada como recurso.

Em vez de:

```text
PSU
 ↓
motherboard
```

a proposta é permitir uma arquitetura futura semelhante a:

```text
POWER SOURCE
     ↓
POWER FABRIC
     │
 ┌───┼─────┐
 ↓   ↓     ↓
NODE GPU STORAGE
```

O sistema conhece:

```text
potência disponível
consumo atual
reservas
picos
domínios elétricos
temperatura
dependências
```

Exemplo:

```text
NODE 3

standby: 5 W
startup_peak: 180 W
sustain: 75 W
maximum: 220 W
```

O Power Fabric pode decidir quando liberar energia ou escalonar partidas.

---

## 8. Power Scheduling

Dispositivos compartilhando cabos, rails ou fontes podem causar transientes e instabilidade.

O sistema deve conhecer também a **topologia energética**.

Exemplo:

```text
POWER SOURCE
│
├── DOMAIN SATA-A
│   ├── HDD0
│   ├── HDD1
│   ├── HDD2
│   └── HDD3
│
└── DOMAIN 12V
    ├── CPU
    └── GPU
```

Em vez de acordar quatro HDDs simultaneamente:

```text
HDD0
   HDD1
      HDD2
         HDD3
```

O sistema pode escalonar o spin-up.

O mesmo princípio pode ser aplicado a CPUs, GPUs e outros aceleradores.

---

## 9. Compute, Power e Thermal como sistema único

Workload gera uma cadeia física:

```text
WORKLOAD
   ↓
COMPUTE
   ↓
POWER
   ↓
THERMAL
   ↓
COOLING
```

O OMLS deve aprender e coordenar essas relações.

Exemplo:

```text
prepare cooling
↓
reserve power
↓
ramp compute
```

Em vez de apenas reagir depois da temperatura subir.

---

## 10. Machine Learning operacional

Se o sistema apenas mede e reage, isso é controle adaptativo.

Machine Learning entra quando o sistema:

```text
acumula histórico
detecta padrões
modela relações
faz previsões
altera políticas
```

Exemplo:

```text
GPU0

official maximum:
250 W

learned:

stable sustain:
178 W

best efficiency:
145–165 W

thermal rise above:
205 W

preferred attack:
600 ms
```

A informação aprendida pertence à **máquina real**, não apenas ao modelo genérico da peça.

---

## 11. Behavior Profile

Cada recurso pode possuir um perfil operacional aprendido.

Exemplo:

```text
/node0/gpu0/profile

hardware:
GPU model X

observed:
peak_safe_power = 212 W
preferred_sustain = 165 W
thermal_equilibrium = 71 C
ramp_limit = 35 W/100ms

confidence:
94%
```

O sistema pode manter histórico:

```text
temperatura
consumo
latência
erros
degradação
spin-up time
performance
```

---

## 12. Três planos distintos

É importante separar:

### Control Plane

Determinístico.

Responsável por:

```text
hard limits
safety
drivers
resource allocation
power control
```

### Learning Plane

Responsável por:

```text
telemetria
modelos
predição
behavior profiles
```

### Language Plane

LLM local opcional.

Responsável por:

```text
interpretar hardware
explicar telemetria
analisar documentação
correlacionar dispositivos
sugerir mappings
```

O LLM nunca deve ter autoridade direta irrestrita sobre memória, energia ou registradores críticos.

---

## 13. Papel de um modelo local / Ollama

Um LLM leve pode ajudar principalmente em:

```text
driver recognition
hardware classification
datasheet interpretation
matching existing drivers
community profile analysis
capability inference
human-readable diagnostics
```

Fluxo possível:

```text
UNKNOWN DEVICE
      ↓
collect metadata
      ↓
LLM analysis
      ↓
possible profile
      ↓
safe deterministic tests
      ↓
validated capabilities
```

---

## 14. Hardware distribuído

A máquina lógica não precisa terminar no gabinete.

Computadores inteiros podem participar como nós.

```text
OMLS MASTER
     │
RESOURCE FABRIC
     │
 ┌───┼─────┐
 ↓   ↓     ↓
PC A PC B PC C
```

Cada nó anuncia:

```text
architecture
CPU
RAM
GPU
accelerators
network
power
load
temperature
```

---

## 15. Cluster heterogêneo

O sistema pode combinar:

```text
x86
x86_64
ARM
ARM64
RISC-V
```

Cada nó pode fornecer recursos diferentes.

Externamente, o sistema pode apresentar:

```text
COMPUTE POOL
GRAPHICS POOL
MEMORY POOL
STORAGE POOL
```

Internamente, mantém a hierarquia:

```text
FABRIC
 ↓
NODE
 ↓
PROCESSOR
 ↓
CORE
```

---

## 16. Distância computacional

Um core local e um core remoto não são equivalentes.

O Resource Graph precisa conhecer:

```text
latency
bandwidth
transport
locality
power cost
```

Exemplo:

```text
compute0:
local
latency = low

compute19:
node3
transport = ethernet
latency = higher
```

O scheduler deve usar isso para decidir quando distribuir funções.

---

## 17. Resource Addressing

Recursos locais e remotos podem compartilhar um namespace lógico.

Exemplo:

```text
compute://0
graphics://1
memory://4
storage://2
```

Fisicamente:

```text
graphics://1
 ↓
node://workstation3
 ↓
gpu://0
```

A ideia é semelhante conceitualmente a NAT ou address translation, mas aplicada a recursos de hardware.

---

## 18. Hardware Address / Resource Translation

O software pede:

```text
graphics.compute
```

O sistema resolve:

```text
logical resource
↓
resource resolver
↓
node
↓
transport
↓
physical device
```

A aplicação não precisa necessariamente saber onde o recurso está.

---

## 19. Ethernet como transporte

Ethernet pode ser tratada como mais um tipo de transporte.

Assim como:

```text
PCIe
USB
SPI
I2C
serial
parallel
CXL
Ethernet
```

Cada transporte possui propriedades:

```text
latency
bandwidth
reliability
distance
power cost
```

O scheduler escolhe o caminho adequado.

---

## 20. Máquinas inteiras como módulos

Uma máquina pode entrar ou sair do Resource Fabric.

Exemplo:

```text
NODE JOINED

resources added:
4 CPU cores
8 GB memory
1 GPU
```

ou:

```text
NODE LOST

resources removed:
4 CPU cores
8 GB memory
```

A máquina lógica muda de tamanho dinamicamente.

---

## 21. Hardware antigo e legado

Um dos objetivos do OMLS é reaproveitar hardware abandonado por fabricantes.

O sistema deve separar:

```text
hardware identity
```

de:

```text
hardware capability
```

Uma placa antiga ainda pode oferecer:

```text
display
network
storage
audio
sensor
compute
```

mesmo sem driver oficial moderno.

---

## 22. Community Hardware Profiles

Conhecimento sobre hardware pode ser compartilhado pela comunidade.

Um profile pode descrever:

```text
PCI ID
USB ID
register mapping
protocol
capabilities
power profile
timing
known problems
supported transports
```

Exemplo:

```text
DEVICE:
pci:1234:5678

CLASS:
video.capture

CAPABILITIES:
capture.video
dma
audio.input
```

---

## 23. Conhecimento em três níveis

O sistema pode combinar:

```text
MANUFACTURER PROFILE
+
COMMUNITY PROFILE
+
LOCAL LEARNED PROFILE
```

Ou:

```text
what manufacturer says
+
what community discovered
+
what this machine learned
```

---

## 24. Confiança em profiles

Community profiles precisam possuir níveis de confiança.

Exemplo:

```text
UNKNOWN
EXPERIMENTAL
COMMUNITY TESTED
VERIFIED
STABLE
```

Profiles experimentais devem rodar isolados sempre que possível.

---

## 25. Driver isolation

Drivers comunitários ou experimentais não devem ganhar automaticamente acesso irrestrito ao kernel.

Modelo desejado:

```text
COMMUNITY DRIVER
      ↓
DRIVER SANDBOX
      ↓
CAPABILITY API
      ↓
RESOURCE FABRIC
```

---

## 26. Uso em hardware retrô

OMLS pode permitir reaproveitar computadores antigos como partes funcionais de sistemas modernos.

Exemplo:

```text
modern GPU
    ↓
render 4K scene
    ↓
adapt resolution
    ↓
old computer/display
```

O display antigo não vira 4K.

Ele continua oferecendo:

```text
DISPLAY RESOURCE
```

dentro de seus limites físicos.

Isso permite instalações:

```text
museus
retrocomputing
arte digital
exposições
palco
visual systems
collections
```

---

## 27. Uso científico

Equipamentos standalone de laboratório podem ser integrados como recursos.

Exemplo:

```text
microscope0:
image.capture
focus.control
stage.move

spectrometer0:
spectrum.capture

fpga0:
signal.processing

gpu0:
parallel.compute
```

Então workflows deixam de depender exclusivamente do software proprietário de cada equipamento.

---

## 28. Escala por composição

A filosofia principal é:

> Escalar não precisa significar substituir uma máquina por outra maior.

Também pode significar:

```text
DEVICE A
+
DEVICE B
+
DEVICE C
=
larger logical system
```

Ou:

> composição em vez de substituição.

---

## 29. Escalas do sistema

A mesma abstração pode existir em múltiplos níveis.

```text
CHIP LEVEL
CPU / GPU / FPGA

MACHINE LEVEL
motherboards / computers / servers

ENVIRONMENT LEVEL
labs / studios / clusters / installations
```

Em todos eles o sistema segue:

```text
DISCOVER
DESCRIBE
ADDRESS
ROUTE
SCHEDULE
LEARN
```

---

## 30. Sistemas existentes relevantes

OMLS não precisa reinventar todas as fundações.

### Linux

Já oferece:

```text
driver model
PCI / USB discovery
DeviceTree
runtime PM
CPUFreq
DevFreq
powercap
thermal
hwmon
PMBus
VFIO
UIO
eBPF
```

Linux é o melhor chassi inicial.

### Popcorn Linux

Muito próximo do modelo de cluster heterogêneo.

Permite cooperação entre kernels e execução/migração através de arquiteturas diferentes.

Inspiração para:

```text
distributed execution
heterogeneous compute
```

### LegoOS

Explora hardware desagregado.

Inspiração para:

```text
processor disaggregation
memory disaggregation
storage disaggregation
```

### Barrelfish

Trata multicore e hardware heterogêneo como sistema distribuído.

Inspiração para:

```text
message passing
distributed hardware model
```

### Fuchsia

Possui forte isolamento de drivers e driver framework modular.

Inspiração para:

```text
driver sandboxing
driver topology
binding
```

### NetBSD rump kernels

Permitem executar componentes e drivers de kernel em userspace.

Inspiração para:

```text
driver reuse
kernel component reuse
```

### Zephyr

Excelente device model para microcontroladores.

Possível candidato para:

```text
small OMLS nodes
microcontrollers
hardware bridges
```

### Kubernetes / Slurm

Já trabalham com alocação de recursos distribuídos.

Inspiração para:

```text
resource scheduling
cluster resource registration
```

### CXL

Representa uma direção física real de hardware composável.

Inspiração futura para:

```text
memory pooling
accelerator fabrics
low-latency composability
```

---

## 31. Onde o OMLS difere

Esses sistemas implementam partes da ideia.

OMLS tenta unificar:

```text
hardware discovery
resource abstraction
distributed execution
driver isolation
power management
thermal management
hardware profiles
community knowledge
machine learning
behavior profiles
legacy hardware
dynamic composition
```

em uma única arquitetura operacional.

---

## 32. Estratégia inicial

Não criar um kernel novo imediatamente.

Primeiro:

```text
LINUX
  ↓
OMLS CONTROL PLANE
```

A maior parte do protótipo deve nascer em userspace.

Só funcionalidades comprovadamente críticas devem descer posteriormente para o kernel.

---

## 33. Arquitetura inicial

```text
                 OMLS MASTER
                     │
               RESOURCE GRAPH
                     │
        ┌────────────┴────────────┐
        │                         │
    OMLS AGENT                OMLS AGENT
    Linux Node A              Linux Node B
        │                         │
 CPU GPU RAM HDD             CPU GPU RAM
```

---

## 34. Primeiro componente: omls-agent

Implementação inicial sugerida:

```text
Go
```

Responsabilidades:

```text
discover local hardware
read /sys
read /proc
udev
PCI
USB
hwmon
CPUFreq
powercap
network
storage
temperatures
```

Saída:

```text
Machine Profile
```

---

## 35. Resource Description Language

Criar formato inicialmente baseado em YAML ou JSON.

Entidades:

```text
DEVICE
RESOURCE
FUNCTION
TRANSPORT
LIMIT
BEHAVIOR
ENVELOPE
NODE
```

Exemplo:

```yaml
node: server0

resources:
  cpu:
    architecture: x86_64
    cores: 8

  gpu0:
    capabilities:
      - graphics
      - compute

  disk0:
    capabilities:
      - storage
```

---

## 36. Fabric Protocol

Primeira versão pode usar:

```text
gRPC
+
mTLS
+
Ethernet
```

Responsável por:

```text
node discovery
resource advertisement
telemetry
function dispatch
health
resource state
```

---

## 37. Resource Graph Scheduler

Deve considerar:

```text
architecture
resource capability
load
latency
bandwidth
temperature
power
locality
availability
```

Aplicações solicitam capabilities.

Exemplo:

```text
FUNCTION:
compile_project

requires:
compute
parallelizable
```

O scheduler decide onde executar.

---

## 38. Primeiro protótipo real

Usar duas máquinas Linux heterogêneas.

Exemplo:

```text
NODE A
Debian

NODE B
Linux Mint

Ethernet
```

O master detecta:

```text
nodes: 2
compute resources: N
memory resources: N
```

Um workload paralelo é distribuído entre as duas.

---

## 39. Primeira aprendizagem operacional

Durante os testes medir:

```text
execution time
CPU frequency
temperature
load
latency
energy
```

Primeira execução:

```text
nodeA = 6 workers
nodeB = 2 workers
```

Depois o sistema aprende que nodeB é menos eficiente.

Nova execução:

```text
nodeA = 7
nodeB = 1
```

Se nodeA começa a superaquecer, redistribui novamente.

Isso já constitui um primeiro OMLS funcional.

---

## 40. Learning Plane inicial

Não começar com redes neurais ou LLM.

Primeiro utilizar:

```text
statistics
EWMA
regression
historical averages
thresholds
correlation
```

Depois:

```text
ML models
prediction
anomaly detection
```

---

## 41. Resource Envelope inicial

Usar recursos já existentes no Linux.

Exemplo:

```text
CPUFreq
cgroups
runtime PM
powercap
thermal
```

Um workload pode declarar:

```text
performance = high
attack = 80%
sustain = 55%
thermal_max = 75C
release = gradual
```

O sistema traduz isso para mecanismos disponíveis.

---

## 42. Hardware experimental

Posteriormente utilizar:

```text
VFIO
UIO
IOMMU
driver sandbox
```

Fluxo:

```text
UNKNOWN DEVICE
      ↓
VFIO/UIO
      ↓
OMLS DRIVER SANDBOX
      ↓
community profile
      ↓
capability discovery
```

---

## 43. LLM local

Ollama ou equivalente entra somente depois da infraestrutura básica.

Funções:

```text
identify possible hardware
read documentation
compare drivers
interpret profiles
generate diagnostics
suggest capability mappings
```

Nunca controlar diretamente hardware crítico.

---

## 44. Power Fabric físico

Deve começar como simulação e telemetria.

Primeiro:

```text
software power budgets
```

Depois:

```text
hwmon
RAPL
PMBus
smart PSU
UPS
```

Só posteriormente considerar hardware próprio.

Arquitetura:

```text
OMLS
 ↓
POWER MANAGER
 ↓
dedicated MCU
 ↓
protected switching hardware
 ↓
device
```

O MCU deve possuir proteções independentes contra:

```text
overcurrent
undervoltage
overtemperature
short circuit
invalid states
```

---

## 45. Patches de kernel

Somente quando userspace demonstrar limitações reais.

Possível progressão:

```text
userspace
↓
eBPF
↓
kernel module
↓
scheduler hooks
↓
device model extensions
↓
optional Linux fork
```

---

## 46. Roadmap inicial

```text
OMLS 0.1
hardware discovery

OMLS 0.2
multi-node fabric

OMLS 0.3
resource graph scheduler

OMLS 0.4
resource envelopes

OMLS 0.5
telemetry + behavior profiles

OMLS 0.6
adaptive scheduling

OMLS 0.7
community hardware profiles

OMLS 0.8
sandboxed experimental drivers

OMLS 0.9
power fabric integration

OMLS 1.0
stable adaptive distributed resource system
```

---

## 47. Definição curta

> **An Operational Machine Learning System is a computing architecture that discovers, models, coordinates and learns the behavior of heterogeneous hardware resources while operating them as a dynamic logical machine.**

Em português:

> **Um Operational Machine Learning System é uma arquitetura computacional que descobre, modela, coordena e aprende o comportamento de recursos heterogêneos de hardware enquanto os opera como uma máquina lógica dinâmica.**

---

## 48. Frases conceituais do projeto

> **A máquina não é definida pelas peças que possui, mas pelas capacidades que consegue oferecer.**

> **A máquina não termina no gabinete.**

> **Hardware fornece capacidades. Software fornece intenção. O sistema conecta os dois.**

> **O kernel não apenas descobre a máquina. Ele aprende a máquina.**

> **Energia também é um recurso.**

> **Escalar não significa necessariamente substituir uma máquina por outra maior. Pode significar conectar máquinas menores até que elas se comportem como uma maior.**

> **Um computador não deveria esquecer como usar uma peça apenas porque a empresa que a fabricou esqueceu dela.**

> **An OS knows how to run software. An OMLS learns how to run the machine.**
