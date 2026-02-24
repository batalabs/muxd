# Security Best Practices for muxd Users

> **TL;DR:** Use environment variables for API keys, set restrictive file permissions, and never commit your config to git.

---

## API Key Management

### Best Practice #1: Use Environment Variables (Recommended)
Store API keys in environment variables instead of config files:

```bash
# macOS / Linux
export ANTHROPIC_API_KEY="sk-ant-..."
export OPENAI_API_KEY="sk-..."
export BRAVE_SEARCH_API_KEY="..."
export TELEGRAM_BOT_TOKEN="123456:ABC-..."

muxd
```

```powershell
# Windows PowerShell
$env:ANTHROPIC_API_KEY = "sk-ant-..."
$env:OPENAI_API_KEY = "sk-..."
$env:BRAVE_SEARCH_API_KEY = "..."
$env:TELEGRAM_BOT_TOKEN = "123456:ABC-..."

muxd
```

**Why?** Environment variables are:
- ✅ Not written to disk
- ✅ Not vulnerable to file permission mistakes
- ✅ Easy to rotate (just restart)
- ✅ Better for CI/CD and containerization

### Best Practice #2: Secure Your Config File
If you must use the config file, protect it:

```bash
# After setting keys via /config set
chmod 600 ~/.config/muxd/config.json

# Verify (should show rw-------)
ls -la ~/.config/muxd/config.json
```

**What does this do?**
- `6` (owner): read + write
- `0` (group): no permissions
- `0` (others): no permissions

Result: Only your user can read the file.

### Best Practice #3: Never Commit config.json
Add to your `.gitignore`:

```bash
# .gitignore
~/.config/muxd/config.json
~/.config/muxd/
```

Or use a global gitignore:

```bash
echo "~/.config/muxd/" >> ~/.gitignore_global
git config --global core.excludesfile ~/.gitignore_global
```

**Why?** If you accidentally commit it:
- ❌ All your API keys are in git history forever
- ❌ Anyone with repo access gets your keys
- ❌ Impossible to fully delete (git history)

### Best Practice #4: Rotate Compromised Keys
If you suspect a key was leaked:

1. **Revoke immediately:**
   - Anthropic: console.anthropic.com → API keys
   - OpenAI: platform.openai.com → API keys
   - Telegram: @BotFather → disable bot
   - Brave: asearch.brave.com → Manage API keys

2. **Generate new key:**
   - Follow provider's instructions

3. **Update muxd:**
   ```
   /config set anthropic.api_key <new-key>
   ```

4. **Restart muxd:**
   - Exit and restart to ensure old key is flushed from memory

---

## File Permissions

### Safe Setup Checklist

```bash
# 1. Verify config directory is private
ls -ld ~/.config/muxd/
# Should show: drwx------

# 2. Verify config file is private
ls -l ~/.config/muxd/config.json
# Should show: -rw-------

# 3. Verify database is private
ls -l ~/.local/share/muxd/
# Should show: drwx------
```

### What to Watch For

❌ **INSECURE:**
```
drwxr-xr-x  ~/.config/muxd/
-rw-r--r--  ~/.config/muxd/config.json
```
> Group and others can read your keys!

✅ **SECURE:**
```
drwx------  ~/.config/muxd/
-rw-------  ~/.config/muxd/config.json
```
> Only you can read.

### Fix Insecure Permissions

If muxd shows a warning:
```
WARNING: ~/.config/muxd/config.json is readable by others (mode 644). 
Run: chmod 600 ~/.config/muxd/config.json
```

Fix it:
```bash
chmod 600 ~/.config/muxd/config.json
chmod 700 ~/.config/muxd/
chmod 700 ~/.local/share/muxd/
```

---

## Multi-User Systems

### Scenario: Shared Computer

**Problem:** Other users might read your config file.

**Solution:**
1. **Use environment variables only** (recommended)
   - No config file with keys = no risk

2. **Encrypt your home directory** (OS-level)
   - Linux: LUKS, eCryptfs
   - macOS: FileVault
   - Windows: BitLocker

3. **Use a separate user account** for muxd
   - Keep work completely isolated

### Scenario: Shared Server

**Problem:** System admins can read all files.

**Solution:**
1. **Don't store production keys** on shared servers
2. **Use environment variables** from secure CI/CD
3. **Run muxd in a container** with secret injection

---

## Telegram Bot Security

### Best Practice #1: Restrict Users
Only allow trusted users to control your bot:

```
/config show
# Look for: telegram.allowed_ids

/config set telegram.allowed_ids 123456789,987654321
```

Get your Telegram user ID:
- Send any message to @IDBot
- It replies with your ID

### Best Practice #2: Use API Keys Wisely
Your Telegram bot token is like an API key:
- ❌ Don't share it
- ❌ Don't commit it to git
- ❌ Don't paste it in Discord/Slack

If leaked:
1. Message @BotFather
2. Select your bot → Edit → Revoke Token
3. Update muxd: `/config set telegram.bot_token <new-token>`

### Best Practice #3: Monitor Commands
If you enable Telegram, be aware:
- Bot can run any shell command
- Rate limiting prevents DOS but not abuse
- Don't add untrusted people to allowed_ids

---

## Project Security

### What muxd Can Access

muxd runs with **your user's permissions**. It can:
- ✅ Read files in your project
- ✅ Create/edit/delete files
- ✅ Run shell commands (npm, git, etc.)
- ✅ Read your `.env` files
- ❌ NOT read ~/.config/muxd/config.json (blocked)

**Be careful with:**
- `.env` files containing secrets → muxd can read them
- Sensitive source code → muxd can read it
- Bash commands → muxd can execute anything you can

### Using muxd with .env Files

**Scenario:** You have a `.env` with secrets

```env
DATABASE_PASSWORD=secret123
API_KEY_PROD=sk-...
```

**Risk:** If you ask muxd to "read .env", it will see all secrets.

**Mitigation:**
1. **Don't ask muxd to read .env directly**
2. **Use a .env.example** with placeholders:
   ```env
   DATABASE_PASSWORD=<your-password>
   API_KEY_PROD=<your-key>
   ```
3. **Ask muxd to work with .env.example instead**

---

## Undo/Redo Security

### What Gets Stored
When muxd creates/edits files, it stores undo/redo checkpoints:
- Location: `.git/refs/muxd-*` (git stash-like)
- Stored in your project's git repo
- Contains file contents at time of change

### Is It Safe?
✅ **Yes, if your git repo is secure:**
- Private GitHub repo → Safe
- Public GitHub repo → Anyone can see old file states

❌ **Risk if repo is public:**
- Secrets that were removed → Still visible in stash
- Private code that was deleted → Still visible

### Best Practice
1. **Make repos private** if they have secrets
2. **Don't remove secrets, rotate them:**
   - ❌ Remove secret from code, push
   - ✅ Rotate secret at provider, then remove code
3. **Use BFG Repo-Cleaner** if you accidentally leaked a secret

---

## Incident Response

### "I Think My API Key Was Leaked"

**Immediate (5 minutes):**
1. Revoke the key at the provider
2. Generate a new key
3. Restart muxd: `muxd /exit` then `muxd`

**Short-term (next day):**
1. Check provider's logs for unauthorized usage
2. Monitor your account for unusual activity
3. Update other tools that use the key

**Long-term:**
1. Add key rotation to your security checklist (quarterly)
2. Use secrets manager (1Password, LastPass) to auto-rotate
3. Enable MFA on provider accounts

### "I Committed My Config to GitHub"

**Immediate (1 hour):**
1. Revoke all keys (API providers, Telegram)
2. Delete the commit from git history:
   ```bash
   git filter-branch --force --index-filter \
     'git rm --cached --ignore-unmatch ~/.config/muxd/config.json' \
     --prune-empty --tag-name-filter cat -- --all
   git push --force --all
   ```
3. Generate new keys

**Long-term:**
1. Add `~/.config/muxd/` to `.gitignore`
2. Set up git hooks to prevent this:
   ```bash
   git config core.hooksPath ./hooks
   ```

---

## Monitoring & Logging

### Check for Suspicious Activity

**Recently used keys:**
```bash
grep -l "ANTHROPIC_API_KEY\|OPENAI_API_KEY" ~/.bash_history ~/.zsh_history
# Note: keys might be in command history (not ideal)
```

**Recent API usage (at provider):**
- Anthropic: console.anthropic.com → Dashboard
- OpenAI: platform.openai.com → Usage
- Telegram: @BotFather → Check bot stats

**Session logs (muxd):**
```bash
ls -la ~/.local/share/muxd/
# muxd.db contains all your session history
```

---

## FAQ

**Q: Can muxd see my API keys if I use env vars?**  
A: muxd can read them at startup (standard practice), but they're not stored to disk.

**Q: Is my Telegram bot data encrypted?**  
A: Telegram messages are encrypted in transit. muxd stores responses locally in plaintext.

**Q: Can someone on the internet access my muxd daemon?**  
A: No. It only listens on `localhost` (127.0.0.1). Not accessible remotely.

**Q: What if my computer is stolen?**  
A: With full disk encryption enabled: keys are safe. Without it: they can be read.

**Q: Can I use muxd in a Docker container?**  
A: Yes. Mount config and data as volumes. Use env vars for secrets (recommended).

**Q: Should I grant muxd shell access?**  
A: Yes, that's its purpose. But only if you trust the AI model provider.

**Q: How often should I rotate API keys?**  
A: At minimum quarterly. More often if:
  - You suspect compromise
  - You accidentally exposed them
  - Provider recommends it

---

## Resources

- **Anthropic API Docs:** https://docs.anthropic.com
- **OpenAI API Docs:** https://platform.openai.com/docs
- **OWASP Top 10:** https://owasp.org/www-project-top-ten/
---

**Last Updated:** 2026-02-23
