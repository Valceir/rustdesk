# VSUP – cliente Windows

Fork do RustDesk com a configuração da V.S.A. Solutions embutida. Desenvolvido pela V.S.A. Solutions.

## O que muda em relação ao RustDesk

| Arquivo | Mudança |
|---|---|
| `src/common.rs` | `apply_vsup_config()`: nome VSUP, servidor, chave e painel fixos; telas de servidor ocultas; sem atualização automática |
| `src/client.rs` | Não envia o token ao hbbs (o hbbs open source não faz a troca de chaves e a conexão travava com analista logado) |
| `src/platform/windows.rs` | `vsup_agent_cmd()`: a instalação roda `vsup-agent.exe install` e a desinstalação roda `uninstall` |
| `vsup/agent/` | Agente VSUP em Go: serviço `VSUPAgent` com inventário e canal de comandos com o painel |
| `res/`, `flutter/assets/`, `flutter/windows/runner/` | Ícones, logos e dados do executável |
| `.github/workflows/vsup-windows.yml` | Compila só o Windows x64, renomeia o executável para `VSUP.exe` (o instalador exige) e publica a release `vsup-X.Y.Z` |

## Publicar uma versão

1. Altere `VSUP_VERSAO` (ex.: `1.0.1`) e faça o push na branch `vsup`.
2. O GitHub Actions compila (40–60 min) e publica a release `vsup-1.0.1` com `VSUP-1.0.1-windows.exe`.
3. O painel (`/baixar`) passa a entregar a versão nova sozinho.

## Atualizar a base do RustDesk

Faça o rebase destes commits sobre a tag nova do RustDesk e teste antes de publicar.
O servidor (`rustdesk-server`) é fixado no repositório do painel.

## Agente VSUP

- Serviço `VSUPAgent` (LocalSystem). Log em `C:\ProgramData\VSUP\agent.log`.
- Lê o ID do VSUP no arquivo de configuração do serviço e se registra no painel (`/api/agent/register`).
- Envia o inventário ao iniciar e a cada 6 horas, e espera comandos por long-poll (`/api/agent/poll`).
- Só executa comandos de uma lista fechada no próprio código. O painel não consegue mandar comando livre.
- Testes: `vsup-agent.exe inventory` (mostra o JSON) e `vsup-agent.exe debug` (roda no console).
- Requer Windows 10 ou mais novo (Go 1.24).
