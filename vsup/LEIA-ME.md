# VSUP – cliente Windows

Fork do RustDesk com a configuração da V.S.A. Solutions embutida. Desenvolvido pela V.S.A. Solutions.

## O que muda em relação ao RustDesk

| Arquivo | Mudança |
|---|---|
| `src/common.rs` | `apply_vsup_config()`: nome VSUP, servidor, chave e painel fixos; telas de servidor ocultas; sem atualização automática |
| `src/client.rs` | Não envia o token ao hbbs (o hbbs open source não faz a troca de chaves e a conexão travava com analista logado) |
| `src/platform/windows.rs` | `vsup_agent_cmd()`: a instalação roda `vsup-agent.exe install` e a desinstalação roda `uninstall` |
| `src/platform/macos.rs` | `vsup_start_agent()`: o serviço do VSUP (root) roda `vsup-agent install` ao iniciar |
| `vsup/agent/` | Agente VSUP em Go: serviço `VSUPAgent` com inventário e canal de comandos com o painel |
| `res/`, `flutter/assets/`, `flutter/windows/runner/` | Ícones, logos e dados do executável |
| `.github/workflows/vsup-windows.yml` (nome histórico; compila todas as plataformas) | Compila Windows x64 (renomeia para `VSUP.exe`, que o instalador exige), Android (APK universal arm64+armv7) e macOS (Apple Silicon e Intel) e publica tudo na release `vsup-X.Y.Z` |
| `flutter/android/` | `applicationId` `br.com.vsasolutions.vsup`, nome VSUP, ícones |
| `flutter/macos/` | `PRODUCT_NAME` VSUP (`VSUP.app`), bundle `br.com.vsasolutions.vsup`, ícone; `build.py` copia o `service` para `VSUP.app` |

## Publicar uma versão

1. Altere `VSUP_VERSAO` (ex.: `1.0.1`) e faça o push na branch `vsup`.
2. O GitHub Actions compila (60–90 min) e publica a release `vsup-1.0.1` com `VSUP-1.0.1-windows.exe`,
   `VSUP-1.0.1-android.apk`, `VSUP-1.0.1-macos-apple-silicon.dmg` e `VSUP-1.0.1-macos-intel.dmg`.
   Se o Android ou o Mac falharem, o Windows é publicado mesmo assim.
3. O painel (`/baixar`) passa a entregar a versão nova sozinho.

## Atualizar a base do RustDesk

Faça o rebase destes commits sobre a tag nova do RustDesk e teste antes de publicar.
O servidor (`rustdesk-server`) é fixado no repositório do painel.

## Agente VSUP

- Serviço `VSUPAgent` (LocalSystem). Log em `C:\ProgramData\VSUP\agent.log`.
- Lê o ID do VSUP no arquivo de configuração do serviço e se registra no painel (`/api/agent/register`).
- Envia o inventário ao iniciar e a cada 6 horas, e espera comandos por long-poll (`/api/agent/poll`).
- Só executa as ações de `actions.go` (lista fechada). O painel não consegue mandar comando livre.
- Ações demoradas (sfc, otimizar disco) rodam em paralelo; reiniciar e desligar avisam o usuário com 1 minuto.
- Desempenho em tempo real (`live.go`): amostra a cada 1 s só quando o painel pede (`live_seconds`); cada envio renova o prazo e a coleta para sozinha sem ninguém olhando.
- macOS: LaunchDaemon `br.com.vsasolutions.vsup.agent` (root), binário em `VSUP.app/Contents/MacOS/vsup-agent`.
  É instalado pelo serviço do VSUP quando ele inicia (ou à mão: `sudo .../vsup-agent install`). Reinicia
  sozinho quando o VSUP.app é atualizado e se remove quando o app é apagado. Log em `/Library/Logs/VSUP/agent.log`. Ações: as mesmas do Windows,
  menos otimizar disco, sfc e Windows Update; mais `mac_update` (`softwareupdate --list`).
- Testes: `vsup-agent.exe inventory` (mostra o JSON) e `vsup-agent.exe debug` (roda no console).
- Requer Windows 10 ou mais novo (Go 1.24).

## Secrets necessários (Settings → Secrets and variables → Actions)

| Secret | Para quê |
|---|---|
| `VSUP_ANDROID_KEYSTORE` | Keystore do Android em base64 (alias `vsup`). **Nunca troque**: os celulares só aceitam atualização assinada com a mesma chave. |
| `VSUP_ANDROID_KEY_PASSWORD` | Senha do keystore e da chave |

O macOS sai com assinatura ad-hoc (sem conta Apple). Na primeira abertura: Ajustes do Sistema →
Privacidade e Segurança → "Abrir mesmo assim". Com uma conta Apple Developer dá para assinar e notarizar.
