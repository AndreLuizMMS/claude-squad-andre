# Módulo Coordenação de Sessões — Spec Conceitual

> Fork do `claude-squad`. Este documento descreve **comportamento esperado**, não implementação.

## Visão geral

O coordenador atual acopla duas responsabilidades que não precisam andar juntas: **gerenciar vários agentes em paralelo** e **isolar o código de cada agente em uma cópia separada do repositório**. Quem quer apenas a primeira é obrigado a aceitar a segunda, e paga o preço: branches órfãs acumuladas, diretórios de trabalho duplicados, e edições que não aparecem no estado real do repositório sem uma etapa extra de integração.

O coordenador também assume que existe um "repositório atual": ele só é lançado de dentro de um, e toda sessão nasce naquele mesmo lugar. Isso impede o cenário de quem trabalha em vários produtos simultaneamente.

Este módulo resolve os dois problemas:

1. **Isolamento vira escolha da sessão**, não imposição do programa.
2. **O diretório de trabalho vira propriedade da sessão**, não do processo.

O resultado é um coordenador que funciona como painel de controle de uma estação de trabalho inteira, e não como acessório de um repositório específico.

---

## Atores e capacidades

| Ator | O que pode fazer | O que não pode fazer |
|---|---|---|
| **Desenvolvedor** | Criar, nomear, acompanhar, entrar, pausar, retomar e encerrar sessões; escolher diretório e modo de isolamento de cada sessão | Alterar diretório ou modo de uma sessão já criada; operar duas sessões em primeiro plano ao mesmo tempo |
| **Agente** | Executar comandos e editar arquivos dentro do diretório que a sessão definiu; solicitar input ao desenvolvedor | Escolher seu próprio diretório ou modo; enxergar ou interferir em outras sessões |
| **Supervisor de sessões** | Observar todas as sessões e sinalizar mudanças de estado; responder automaticamente a prompts quando o modo automático estiver ligado | Alterar arquivos, integrar código ou tomar qualquer decisão sobre versionamento |

---

## Entidades principais

**Sessão** — unidade central do sistema. Representa um agente trabalhando em um lugar. Carrega título, diretório de trabalho, modo de isolamento, programa executado e estado atual. É criada pelo desenvolvedor, sobrevive ao fechamento do coordenador e só deixa de existir quando encerrada explicitamente.

**Diretório de trabalho** — o lugar onde o agente opera. Pode ou não ser um repositório versionado. É definido no momento da criação da sessão e é imutável depois disso. Duas sessões podem apontar para o mesmo diretório.

**Modo de isolamento** — decide se a sessão trabalha sobre uma cópia própria do repositório ou diretamente sobre o diretório informado. Dois valores possíveis:

- **Isolado** — a sessão recebe uma cópia de trabalho própria e uma linha de trabalho separada. As alterações do agente ficam invisíveis para o diretório original até serem integradas. Exige que o diretório seja um repositório versionado.
- **Direto** — a sessão opera sobre o diretório informado, no estado em que ele estiver. As alterações do agente aparecem imediatamente para qualquer outra ferramenta apontando para o mesmo lugar. Não exige versionamento e nunca altera o versionamento.

O modo é escolhido na criação e é imutável. *Justificativa:* converter uma sessão de um modo para o outro exigiria decidir o destino de trabalho não integrado — uma decisão que pertence ao desenvolvedor, não ao coordenador.

**Linha de trabalho** — a identidade de versionamento de uma sessão isolada. Existe apenas no modo isolado. Sobrevive à pausa da sessão e é o que permite retomar o trabalho depois.

**Programa** — o agente executado na sessão (Claude Code por padrão, ou outro definido em perfil). Escolhido na criação junto com o diretório e o modo.

### Relações

- Uma **sessão** tem exatamente um **diretório**, um **modo** e um **programa**.
- Uma **sessão isolada** tem exatamente uma **linha de trabalho**; uma sessão direta não tem nenhuma.
- Um **diretório** pode ter várias **sessões** apontando para ele, em qualquer combinação de modos.

---

## Máquina de estados — Sessão

| Estado | Significado | Transições possíveis | Quem transita |
|---|---|---|---|
| **Carregando** | Sessão criada, ambiente sendo preparado, agente ainda não respondeu | → Aguardando, → Encerrada | Sistema |
| **Rodando** | O agente está processando | → Aguardando, → Pausada, → Encerrada | Supervisor (detecção), Desenvolvedor (pausar/encerrar) |
| **Aguardando** | O agente terminou ou pediu input; a vez é do desenvolvedor | → Rodando, → Pausada, → Encerrada | Desenvolvedor, Supervisor |
| **Pausada** | O terminal foi liberado; o trabalho está preservado | → Carregando (ao retomar), → Encerrada | Desenvolvedor |
| **Órfã** | O diretório de trabalho não existe mais | → Encerrada | Sistema (detecção), Desenvolvedor |
| **Encerrada** | Estado terminal; a sessão sai da lista | — | — |

**Transições inválidas** são recusadas com mensagem explícita e não alteram nada. Exemplos: retomar uma sessão que não está pausada; pausar uma sessão já pausada; entrar em uma sessão órfã.

**Diferença entre modos na pausa:**

- Modo isolado: pausar libera a cópia de trabalho e preserva a linha de trabalho. Retomar recria a cópia a partir dela.
- Modo direto: pausar apenas encerra o terminal e preserva a entrada na lista. Retomar reabre o terminal no mesmo diretório, no estado em que ele estiver naquele momento. *Justificativa:* sem cópia própria não há o que liberar, mas o valor de "parar de consumir recursos sem perder o registro da sessão" continua existindo. Descartei a alternativa de simplesmente proibir pausa no modo direto, porque isso obrigaria o desenvolvedor a encerrar e recriar a sessão perdendo o histórico da lista.

---

## Funcionalidades

### 1. Iniciar o coordenador em qualquer lugar

**Descrição:** o coordenador abre normalmente independente de onde foi lançado, inclusive fora de qualquer repositório.
**Quem aciona:** desenvolvedor, ao abrir o programa.
**Pré-condições:** nenhuma.
**Fluxo principal:** o coordenador abre → carrega as sessões previamente salvas → exibe a lista.
**Fluxos alternativos:** se lançado de dentro de um repositório, esse local passa a ser sugerido como diretório padrão na criação de novas sessões — como conveniência, não como restrição.
**Exceções:** se as sessões salvas não puderem ser lidas, o coordenador abre com lista vazia e informa o problema, sem apagar o registro existente.
**Regras de negócio:** o local de lançamento nunca limita quais diretórios podem ser usados pelas sessões.
**Edge cases:** lançado em diretório sem permissão de leitura → abre normalmente, apenas sem sugestão de diretório padrão.

---

### 2. Criar uma sessão

**Descrição:** o desenvolvedor cria uma nova sessão informando título, diretório, modo de isolamento e programa.
**Quem aciona:** desenvolvedor, a partir da lista de sessões.
**Pré-condições:** nenhuma.

**Fluxo principal:**
1. O desenvolvedor solicita nova sessão.
2. Informa o **título**.
3. Informa o **diretório de trabalho**. O campo vem pré-preenchido com o local de lançamento, quando aplicável, e aceita atalhos de caminho pessoal (`~`) e caminhos relativos.
4. Escolhe o **modo de isolamento**. A escolha inicial segue a preferência salva do desenvolvedor.
5. Escolhe o **programa**, quando houver mais de um perfil configurado.
6. Confirma. A sessão entra em **Carregando**, o ambiente é preparado, o agente sobe e a sessão passa a **Aguardando**.

**Fluxos alternativos:**
- **Modo isolado com escolha de linha de trabalho existente:** o desenvolvedor pode partir de uma linha existente em vez de criar uma nova — comportamento já existente, preservado.
- **Criação com instrução inicial:** o desenvolvedor informa um texto que é entregue ao agente assim que ele sobe. A sessão nasce em **Rodando** em vez de **Aguardando**.

**Exceções:**
- Diretório não existe → a criação é bloqueada com mensagem clara; o desenvolvedor volta ao campo de diretório com o valor preservado.
- Diretório existe mas não é gravável → criação bloqueada com mensagem específica.
- Modo isolado sobre diretório não versionado → criação bloqueada, com sugestão explícita de usar modo direto.
- Falha ao subir o agente → a sessão é descartada e todo ambiente parcialmente criado é desfeito. Nada fica pela metade.

**Regras de negócio:**
- Título e diretório são obrigatórios; modo e programa têm padrão.
- Diretório e modo são imutáveis após a criação.
- Títulos duplicados são permitidos; a identidade da sessão não depende do título.
- O modo direto nunca cria, altera ou remove nada relacionado a versionamento.

**Edge cases:**
- Diretório informado é um arquivo, não uma pasta → tratado como diretório inexistente.
- Duas sessões no mesmo diretório em modo direto → permitido, sem aviso. *Justificativa:* impedir seria reintroduzir a tutela que motivou o fork; o desenvolvedor é responsável por coordenar o próprio trabalho.
- Diretório é um repositório versionado mas o desenvolvedor escolhe modo direto → permitido e sem aviso. É o caso de uso central deste fork.

---

### 3. Acompanhar o estado das sessões

**Descrição:** o desenvolvedor vê, de relance, o que cada agente está fazendo.
**Quem aciona:** supervisor, continuamente; o desenvolvedor apenas observa.
**Pré-condições:** existir ao menos uma sessão não encerrada.
**Fluxo principal:** o supervisor observa cada sessão em intervalo regular → detecta mudança de atividade do agente → atualiza o estado → a lista reflete a mudança.
**Fluxos alternativos:** com modo automático ligado, o supervisor responde por conta própria às solicitações do agente e a sessão permanece em **Rodando**.
**Exceções:** se uma sessão deixar de responder, ela é marcada como **Órfã** em vez de ficar presa em **Rodando** indefinidamente.
**Regras de negócio:** o acompanhamento funciona de forma idêntica nos dois modos de isolamento. *Esta é a capacidade central que justifica o coordenador existir em vez de vários terminais soltos.*
**Edge cases:**
- Sessão pausada não é observada e não consome recursos.
- Diretório removido enquanto a sessão roda → estado **Órfã** na próxima verificação.
- Muitas sessões simultâneas → o intervalo de verificação é uniforme e não degrada com a quantidade.

---

### 4. Entrar e sair de uma sessão

**Descrição:** o desenvolvedor assume o terminal de um agente para conversar com ele, e depois volta à lista.
**Quem aciona:** desenvolvedor.
**Pré-condições:** sessão em **Rodando** ou **Aguardando**.
**Fluxo principal:** desenvolvedor seleciona a sessão e entra → o terminal do agente ocupa a tela → ele interage → sai e volta à lista, com a sessão continuando a rodar em segundo plano.
**Fluxos alternativos:** o desenvolvedor pode alternar entre a pré-visualização do terminal e a visão de alterações sem entrar na sessão.
**Exceções:** tentar entrar em sessão pausada → recusado, com sugestão de retomar primeiro. Tentar entrar em sessão órfã → recusado, com sugestão de encerrar.
**Regras de negócio:** sair da sessão nunca a interrompe.
**Edge cases:** o agente termina enquanto o desenvolvedor está dentro → o estado muda em segundo plano e já aparece atualizado ao voltar à lista.

---

### 5. Revisar alterações

**Descrição:** o desenvolvedor vê o que o agente mudou, sem sair do coordenador.
**Quem aciona:** desenvolvedor, a partir da lista.
**Pré-condições:** sessão não pausada e não órfã.

**Fluxo principal por modo:**
- **Isolado:** exibe as alterações da sessão em relação ao ponto de partida da linha de trabalho — comportamento atual, preservado.
- **Direto:** exibe as alterações não integradas presentes no diretório naquele momento.

**Fluxos alternativos:** o desenvolvedor rola a visão de alterações sem entrar na sessão.

**Exceções:**
- Modo direto sobre diretório não versionado → a visão informa que não há base de comparação disponível. Não é erro.
- Diretório inacessível → a visão informa indisponibilidade e a sessão é reavaliada como possivelmente órfã.

**Regras de negócio:** no modo direto, as alterações exibidas **não são exclusivas do agente** — incluem qualquer edição feita no diretório, inclusive pelo próprio desenvolvedor ou por outra sessão. Isso é declarado na interface. *Justificativa:* atribuir alterações ao agente exigiria rastrear autoria de cada edição, complexidade desproporcional para uma visão de conferência rápida.

**Edge cases:**
- Volume muito grande de alterações → a visão é truncada com indicação explícita de truncamento.
- Duas sessões diretas no mesmo diretório → ambas exibem exatamente as mesmas alterações. Comportamento correto e esperado.

---

### 6. Pausar e retomar

**Descrição:** o desenvolvedor libera os recursos de uma sessão sem perder o trabalho nem o registro dela.
**Quem aciona:** desenvolvedor.
**Pré-condições:** para pausar, sessão em **Rodando** ou **Aguardando**. Para retomar, sessão em **Pausada**.

**Fluxo principal:**
- **Pausar (isolado):** o terminal é encerrado, a cópia de trabalho é liberada e a linha de trabalho é preservada e informada ao desenvolvedor.
- **Pausar (direto):** o terminal é encerrado. O diretório fica intocado.
- **Retomar (isolado):** a cópia de trabalho é recriada a partir da linha preservada e o agente sobe novamente.
- **Retomar (direto):** o agente sobe no mesmo diretório, no estado atual dele.

**Exceções:** falha ao liberar a cópia de trabalho → a pausa é abortada e a sessão volta ao estado anterior, sem perda. Falha ao recriar → a sessão permanece pausada e o motivo é informado.

**Regras de negócio:** pausar nunca descarta trabalho. O agente perde seu contexto de conversa ao pausar, nos dois modos — isso é declarado ao desenvolvedor na primeira pausa.

**Edge cases:**
- Retomar sessão direta cujo diretório mudou muito desde a pausa → permitido; o coordenador não compara nem avisa.
- Retomar sessão direta cujo diretório sumiu → transição para **Órfã** em vez de retomada.

---

### 7. Integrar o trabalho de uma sessão

**Descrição:** o desenvolvedor registra e publica o trabalho de uma sessão isolada, ou o retira da cópia de trabalho para inspeção local.
**Quem aciona:** desenvolvedor.
**Pré-condições:** **sessão em modo isolado.** Indisponível em modo direto.
**Fluxo principal:** comportamento atual do coordenador, preservado sem alteração.
**Exceções:** acionado em sessão direta → a ação não é oferecida na interface; se acionada por atalho, é recusada com explicação de uma linha.
**Regras de negócio:** no modo direto, versionamento é responsabilidade integral do desenvolvedor. *Justificativa:* sem cópia própria não existe fronteira segura entre o que o agente fez e o que o desenvolvedor tinha em andamento; agir sobre isso seria adivinhar intenção e arriscar trabalho não salvo. Descartei a alternativa de oferecer registro automático das alterações no modo direto exatamente por esse risco.
**Edge cases:** nenhum específico além dos já existentes no comportamento atual.

---

### 8. Encerrar uma sessão

**Descrição:** o desenvolvedor remove definitivamente uma sessão da lista.
**Quem aciona:** desenvolvedor.
**Pré-condições:** sessão em qualquer estado não terminal.

**Fluxo principal:**
1. O desenvolvedor solicita o encerramento.
2. O coordenador pede confirmação, deixando claro o que será removido.
3. Confirmado: o terminal é encerrado e a sessão sai da lista.

**Diferença por modo:**
- **Isolado:** a cópia de trabalho é removida. A linha de trabalho é preservada. *Justificativa:* apagar trabalho versionado por engano é irreversível; deixar uma linha órfã, não.
- **Direto:** nada além do terminal é tocado. O diretório permanece exatamente como estava.

**Exceções:** falha ao encerrar o terminal → a sessão é removida da lista mesmo assim e o resíduo é reportado, para não travar o desenvolvedor.
**Regras de negócio:** encerramento sempre exige confirmação explícita.
**Edge cases:** encerrar sessão órfã → sempre permitido, sem tentativa de tocar no diretório inexistente.

---

### 9. Restaurar sessões ao reabrir

**Descrição:** ao reabrir o coordenador, as sessões anteriores reaparecem prontas para uso.
**Quem aciona:** sistema, na abertura.
**Pré-condições:** existir registro salvo de sessões.
**Fluxo principal:** o registro é lido → cada sessão é validada → a lista é exibida com os estados corretos.
**Fluxos alternativos:** sessões que estavam pausadas voltam pausadas.
**Exceções:**
- Diretório de uma sessão não existe mais → a sessão aparece como **Órfã**, com o caminho perdido visível para o desenvolvedor entender o motivo.
- Registro corrompido → o coordenador abre com lista vazia, preserva o arquivo original e informa o problema.

**Regras de negócio:** o registro guarda diretório e modo de cada sessão, e sessões salvas por versões anteriores do coordenador — que não tinham modo — são lidas como **isoladas**. *Justificativa:* preserva o comportamento que essas sessões tinham quando foram criadas.

**Edge cases:**
- Sessão isolada cuja cópia de trabalho foi removida por fora → tratada como pausada, já que a linha de trabalho continua existindo.
- Registro com muitas sessões → todas restauradas; nenhum limite artificial.

---

## Notificações

O único canal é a própria interface do coordenador. Não há notificação externa.

| Evento | Receptor | Canal | Comportamento em falha |
|---|---|---|---|
| Agente passou a aguardar input | Desenvolvedor | Estado na lista de sessões | Estado é recalculado na próxima verificação |
| Agente voltou a trabalhar | Desenvolvedor | Estado na lista de sessões | Idem |
| Sessão falhou ao subir | Desenvolvedor | Mensagem imediata na tela | Mensagem persiste até ser dispensada |
| Diretório de uma sessão desapareceu | Desenvolvedor | Estado **Órfã** com caminho visível | Reavaliado a cada verificação |
| Linha de trabalho preservada após pausa | Desenvolvedor | Mensagem informativa com o nome da linha | Nome permanece visível nos detalhes da sessão |

Não há risco de duplicação: notificações refletem estado atual, não eventos acumulados. Uma mesma transição repetida sobrescreve a anterior.

---

## Permissões consolidadas

| Ação | Desenvolvedor | Agente | Supervisor | Condição extra |
|---|---|---|---|---|
| Criar sessão | ✅ | ❌ | ❌ | — |
| Definir diretório e modo | ✅ | ❌ | ❌ | Apenas na criação |
| Entrar na sessão | ✅ | ❌ | ❌ | Sessão ativa |
| Editar arquivos | ✅ | ✅ | ❌ | Agente só dentro do próprio diretório |
| Revisar alterações | ✅ | ❌ | ❌ | Sessão não pausada nem órfã |
| Pausar / retomar | ✅ | ❌ | ❌ | Estado compatível |
| Integrar trabalho | ✅ | ❌ | ❌ | **Somente modo isolado** |
| Encerrar sessão | ✅ | ❌ | ❌ | Com confirmação |
| Alterar estado da sessão | ✅ | ❌ | ✅ | Supervisor apenas por detecção |
| Responder prompts automaticamente | ❌ | ❌ | ✅ | Somente com modo automático ligado |
| Tocar em versionamento | ✅ | ✅ | ❌ | Coordenador nunca age sozinho no modo direto |

---

## Fora de escopo

- **Remover o modo isolado.** Ele continua sendo comportamento válido e padrão. Consequência direta da decisão de manter compatibilidade com o projeto de origem.
- **Integração de trabalho no modo direto.** Consequência direta da ausência de fronteira segura entre trabalho do agente e trabalho do desenvolvedor.
- **Coordenação entre sessões que compartilham diretório.** Consequência direta da decisão de que o coordenador gerencia agentes, não código.
- **Alterar diretório ou modo de uma sessão existente.** Consequência direta da imutabilidade decidida na modelagem da sessão.
- **Atribuição de autoria das alterações no modo direto.** Consequência direta da decisão sobre a visão de alterações.
- **Notificação fora da interface.** Nenhuma funcionalidade definida depende disso.

---

## Resumo de decisões

| Decisão | Escolha | Justificativa | Alternativa descartada |
|---|---|---|---|
| Destino do isolamento por cópia de trabalho | Vira escolha por sessão, com isolado como padrão | Preserva compatibilidade com o projeto de origem e mantém viável absorver melhorias dele | Remover o isolamento por completo |
| Escopo do coordenador | Estação de trabalho inteira, não um repositório | É o requisito central: acompanhar agentes em produtos diferentes ao mesmo tempo | Manter um repositório por instância do coordenador |
| Origem do diretório de trabalho | Informado na criação de cada sessão | Único jeito de suportar múltiplos repositórios sem múltiplas instâncias | Deduzir do local de lançamento |
| Mutabilidade de diretório e modo | Imutáveis após a criação | Alterar exigiria decidir o destino de trabalho não integrado — decisão do desenvolvedor, não do coordenador | Permitir edição posterior |
| Versionamento no modo direto | O coordenador nunca age sozinho | Sem cópia própria não há fronteira segura entre trabalho do agente e do desenvolvedor | Registrar alterações automaticamente |
| Pausa no modo direto | Encerra o terminal e preserva o registro da sessão | Não há cópia para liberar, mas liberar recursos sem perder o registro continua valendo | Proibir pausa no modo direto |
| Visão de alterações no modo direto | Mostra tudo que está no diretório, declarando isso | Atribuir autoria por edição é complexidade desproporcional para conferência rápida | Filtrar apenas o que o agente alterou |
| Duas sessões no mesmo diretório | Permitido, sem aviso | Impedir reintroduziria a tutela que motivou o fork | Bloquear ou avisar |
| Modo isolado sobre diretório não versionado | Bloqueado na criação, com sugestão de modo direto | O modo depende de versionamento para existir; falhar cedo com saída clara é melhor que falhar depois | Aceitar e inicializar versionamento automaticamente |
| Encerramento no modo isolado | Remove a cópia, preserva a linha de trabalho | Apagar trabalho versionado por engano é irreversível; linha órfã não é | Remover tudo |
| Sessões salvas por versões anteriores | Lidas como isoladas | Preserva o comportamento que tinham quando foram criadas | Pedir escolha ao desenvolvedor na abertura |
| Diretório desaparecido | Estado próprio (**Órfã**), com caminho visível | Evita sessão presa em estado ativo indefinidamente e explica o motivo | Encerrar automaticamente |
| Acompanhamento de estado | Idêntico nos dois modos | É a capacidade que justifica o coordenador existir; amarrá-la ao isolamento anularia o fork | Manter acompanhamento apenas no modo isolado |