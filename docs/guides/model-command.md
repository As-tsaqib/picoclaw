# Model Command

`/model` manages the model used by the current conversation session without changing the model for other sessions that use the same agent.

## Commands

- `/model` — open the model dashboard.
- `/model current` — show the effective model for the current session.
- `/model list` — list configured models.
- `/model use <alias|model>` — select a configured model or a model discovered from a configured provider.
- `/model default` — remove the session override and return to the agent default.
- `/model search <query>` — search configured models and models already present in the discovery cache.

`/switch model to <name>` remains available for compatibility, but `/model` is the preferred interface.

## Session-scoped behavior

A model selected with `/model` is stored as a credential-free session override. It survives a PicoClaw restart, but it does not modify the global `AgentInstance` or another session's selection. An in-flight turn keeps the provider/model resolved when that turn started.

`/model default` deletes the session override. The next turn then uses the normal agent configuration and fallback behavior.

## Telegram dashboard

Telegram uses the same native Rich Message/table pattern as `/session`. The dashboard shows the active provider, alias, model ID, scope, status, and known fallback metadata.

Configured and available-model lists show at most five models per page. The table contains the model information; the inline keyboard uses compact numeric selectors:

```text
[ 1 ] [ 2 ] [ 3 ] [ 4 ] [ 5 ]
[ ◀️ ] [ Halaman 1/4 ] [ ▶️ ]
```

The active row is marked with `✅`. A discovered row that also exists in PicoClaw configuration is marked with `★ Configured`.

Model IDs are kept server-side. Telegram callback data contains only a short opaque token and an internal selector, so long model names are not exposed in callback payloads and stay below Telegram's callback-data limit.

## Configured vs. Available Models

**Configured Models** reads PicoClaw's `model_list`. These entries can use their configured aliases and provider/fallback settings.

**Available Models** performs live model discovery using providers that are already configured. Discovery does not send a chat/inference request and does not automatically write discovered models into `model_list`.

Discovery currently follows PicoClaw's supported provider-listing paths, including OpenAI-compatible `/models`, Ollama model tags, NearAI, and Antigravity. Providers without a reliable listing API remain usable through Configured Models.

Discovery results are cached briefly (five minutes) to avoid repeatedly calling provider APIs. Telegram's **Refresh** action bypasses the cache for the selected provider.

## Security and failure behavior

Provider credentials and API keys are never stored in the session model override or shown in the dashboard. A discovered model reuses the credential/base URL of the configured provider source that discovered it.

Before an override is saved, PicoClaw validates that the configured provider source still exists and can initialize the selected model. If validation fails, the previous model remains active.

Interactive callbacks are bound to the originating user, channel/account, chat/topic, agent, and session scope. Invalid, foreign, or expired callbacks are rejected internally and are not sent to the LLM or conversation history.

A discovery failure does not disable `/model`: Configured Models and the current session model remain usable, and provider-specific discovery errors are shown without changing the active model.
