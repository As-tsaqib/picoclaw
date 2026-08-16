> Back to [README](../../../README.md)

# Telegram

The Telegram channel uses long polling via the Telegram Bot API for bot-based communication. It supports text messages, media attachments (photos, voice, audio, documents), voice transcription ([setup](../../guides/providers.md#voice-transcription)), and built-in command handling.

## Configuration

### Visual dashboard

Open **Dashboard → Channels → Telegram** to configure the bot. The **Ephemeral Group Replies** card provides the mode, command list, and personal-session isolation controls without requiring Raw JSON edits.

Start with **Selected Commands** so only intentional slash-command flows become private. **Off** keeps normal Telegram behavior, while **All Group Responses** attempts private delivery for every eligible group response and may require the bot to be a group administrator.

Save the channel configuration after editing it. A gateway restart may be required before the change takes effect; the dashboard displays a restart-required notification when applicable.

### Raw JSON

Advanced or manual configurations remain supported. Channel-specific values, including `ephemeral`, belong under `settings`:

```json
{
  "channel_list": {
    "telegram": {
      "enabled": true,
      "type": "telegram",
      "allow_from": ["123456789"],
      "settings": {
        "token": "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
        "proxy": "",
        "use_markdown_v2": false,
        "media_group_delay_ms": 500,
        "ephemeral": {
          "mode": "commands",
          "commands": ["clear", "help"],
          "personal_session_isolation": true
        }
      }
    }
  }
}
```

| Field                                         | Type   | Required | Description                                                                   |
| --------------------------------------------- | ------ | -------- | ----------------------------------------------------------------------------- |
| enabled                                       | bool   | Yes      | Whether to enable the Telegram channel                                        |
| settings.token                                | string | Yes      | Telegram Bot API Token                                                        |
| allow_from                                    | array  | No       | Allowlist of user IDs; empty means all users are allowed                      |
| settings.proxy                                | string | No       | Proxy URL for connecting to the Telegram API (e.g. http://127.0.0.1:7890)     |
| settings.use_markdown_v2                      | bool   | No       | Enable Telegram MarkdownV2 formatting                                         |
| settings.media_group_delay_ms                 | int    | No       | Idle delay before processing Telegram media groups/albums. Defaults to 500 ms |
| settings.ephemeral.mode                       | string | No       | Private group reply policy: `off` (default), `commands`, or `all`             |
| settings.ephemeral.commands                   | array  | No       | Command names used by `commands` mode; empty means every registered command   |
| settings.ephemeral.personal_session_isolation | bool   | No       | Must remain `true` while ephemeral mode is enabled; defaults to `true`        |

## Setup

1. Search for `@BotFather` in Telegram
2. Send the `/newbot` command and follow the prompts to create a new bot
3. Obtain the HTTP API Token
4. Fill in the Token in the visual dashboard or Raw JSON configuration
5. (Optional) Configure `allow_from` to restrict which user IDs can interact (you can get IDs via `@userinfobot`)

## Built-in Commands

Telegram auto-registers PicoClaw's top-level bot commands at startup, including `/start`, `/help`, `/show`, `/list`, `/session`, and `/use`.

Named conversation sessions:

- In a normal group/topic route, `/session` or `/session list` shows only sessions from that exact safe route scope.
- A Telegram **private chat automatically becomes `👤 Session Saya`**. No user registration is required. The list can include that numeric `from.id` user's verified private, sender-owned group/topic, and personal ephemeral sessions from the same bot account and agent. Sessions owned by another user and shared group sessions without provable ownership stay hidden.
- One optional superadmin can be configured in **Dashboard → Channels → Telegram → Session Superadmin**. Only the configured numeric Telegram User ID, in a private chat with the configured bot account and agent, receives `🌐 Mode Global Superadmin`. If no superadmin is configured or it is disabled, nobody receives global access. A superadmin speaking in a group/topic still gets the normal local route scope.
- `/session current` shows the active name, short ID, origin channel/chat/topic, verified owner, and mode (`Personal`, `Superadmin`, or local `Route`).
- `/session new [name]` creates a separate session and activates it only for the current route/dashboard mapping.
- `/session rename <new name>` renames the active session.
- `/session use <number|short-id>` switches without using an inline button.

Personal-dashboard and superadmin selections use separate durable mappings. Attaching a group/topic session from private chat **does not change the active mapping of the origin group/topic**; subsequent replies stay in the private chat while history continues in the selected session. The mapping survives gateway restarts. The in-flight turn keeps the session selected when that turn began, so a concurrent switch only affects the next turn. `/session new` remains separate from `/new`, `/clear`, and `/reset`.

Session metadata stores the user-facing name/source plus verified origin metadata (owner Telegram user ID when available, channel/bot account, routing account, agent, chat, topic, sender, and route). Older sessions keep their history and receive fallback names. Ownerless legacy sessions are excluded from personal dashboards unless ownership can be proven from their old scope. Global `Legacy/Unknown` rows are hidden by default and require the explicit dashboard opt-in.

The session menu is owner-bound. Inline callbacks use short opaque process-local menu tokens instead of session keys, expire after 15 minutes, and are revalidated against the Telegram user, chat, bot/channel account, routed agent, dashboard mode, and menu location. Expired/malformed/foreign callbacks are rejected without exposing session names or IDs and never enter the LLM or conversation history. `✖️ Tutup` only removes the menu keyboard; it never deletes a session.

On Bot API 10.2 servers, `/session` and structured informational commands such as `/help`, `/show`, `/list`, and `/context` use `sendRichMessage` with native `InputRichMessage`/table blocks. The native table and `reply_markup` are sent in the same request. Session pages contain five sessions and at most five numbered buttons per row. Older clients may display a reduced representation, and servers that reject Rich Messages automatically receive a readable text/Markdown fallback with the same inline controls.

Skill-related commands:

- `/list skills` lists the installed skills visible to the current agent.
- `/list mcp` lists configured MCP servers and whether they are deferred/connected.
- `/show mcp <server>` lists the active tools for a connected MCP server.
- `/use <skill> <message>` forces a skill for a single request.
- `/use <skill>` arms the skill for your next message in the same chat.
- `/use clear` clears a pending skill override.

Examples:

```text
/list skills
/list mcp
/show mcp github
/use git explain how to squash the last 3 commits
/use git
explain how to squash the last 3 commits
```

## Ephemeral Group Replies (Bot API 10.2)

Ephemeral replies are disabled by default. They apply only to groups and supergroups; private chats keep their normal delivery behavior.

In the visual dashboard, open **Dashboard → Channels → Telegram → Ephemeral Group Replies**. The command field accepts names with or without a leading `/`, normalizes them to lowercase, and leaves an empty list to mean every registered PicoClaw command. Personal session isolation is locked on whenever ephemeral replies are enabled.

```json
{
  "channel_list": {
    "telegram": {
      "enabled": true,
      "type": "telegram",
      "settings": {
        "ephemeral": {
          "mode": "commands",
          "commands": ["clear", "help"],
          "personal_session_isolation": true
        }
      }
    }
  }
}
```

- `off` preserves the existing Telegram payloads and group session keys.
- `commands` marks only the listed slash commands as ephemeral. An empty list accepts every Telegram bot-command entity and marks every registered PicoClaw command as ephemeral in Telegram's command menu.
- `all` makes every eligible group response ephemeral.
- Personal session isolation prevents two members of one group from sharing ephemeral history. The private key always contains the verified group ID and raw Telegram sender ID, even when public `identity_links` are configured. Disabling it while the feature is enabled is rejected as unsafe.
- `/clear` clears only the requesting member's private session. PicoClaw does not register a built-in `/new` command; `/new` is handled as ordinary model input unless a separately installed command handles it. A routed agent gets a separate private session for the same group and user.
- Private history is still stored by the configured PicoClaw session backend so the interaction can continue. It is separate from that user's public group history and from other users' private history. Workspace `memory/MEMORY.md` remains agent/workspace-wide; do not save secrets there from an ephemeral turn. Private turns are excluded from the optional evolution-learning sink.

The receiver is always derived from the verified Telegram update; model output and arbitrary metadata cannot select it. If Telegram rejects private delivery, PicoClaw fails closed and does not resend the content publicly. Configuration changes may require a gateway restart.

Telegram permits non-admin bots to send an ephemeral reply for 15 seconds after an incoming ephemeral message or callback query. Administrators can send to a non-bot member without that short-lived authorization, but delivery is never guaranteed, especially while the user is offline. Replies to an existing ephemeral interaction remain private in `commands` mode even when the follow-up text is not itself a slash command.

Bot API 10.2 does not expose an ephemeral receiver on `sendRichMessage`, `sendMediaGroup`, or streaming drafts. Consequently, ephemeral tables use a receiver-aware preformatted fallback, albums are sent as individual private media, and streaming is disabled for the private session. Normal public rich tables continue to use native rich messages. Reasoning-channel side output and model-triggered reactions are also disabled for private turns. Legacy asynchronous tool follow-up turns are suppressed because their system-message route cannot carry the process-local private capability; an async tool's direct `ForUser` result can still use the verified private route while it remains valid.

PicoClaw verifies that Telegram's response contains an ephemeral message ID and the expected receiver. Operators using a custom `base_url` must ensure that it implements Bot API 10.2 and rejects unsupported parameters. A non-conforming server that silently ignores `receiver_user_id` can only be detected after it responds and cannot be made safe by a client-side retry policy.

## Advanced Formatting

You can set `use_markdown_v2: true` to enable enhanced formatting options. This allows the bot to utilize the full range of Telegram MarkdownV2 features, including nested styles, spoilers, and custom fixed-width blocks.

```json
{
  "channel_list": {
    "telegram": {
      "enabled": true,
      "type": "telegram",
      "allow_from": ["YOUR_USER_ID"],
      "settings": {
        "token": "YOUR_BOT_TOKEN",
        "use_markdown_v2": true
      }
    }
  }
}
```
