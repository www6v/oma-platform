package installbridge

import (
	"encoding/base64"
	"encoding/json"
	"html"
)

// GitHubManifestStartPage renders the auto-POST manifest form.
func GitHubManifestStartPage(form ManifestForm) string {
	manifestB64 := base64.StdEncoding.EncodeToString([]byte(form.ManifestJSON))
	escapedName := html.EscapeString(form.PersonaName)
	escapedState := html.EscapeString(form.State)
	manifestJSON, _ := json.Marshal(manifestB64)
	return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Create GitHub App — ` + escapedName + `</title>
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <style>
    body { font: 15px/1.5 system-ui, sans-serif; max-width: 560px; margin: 60px auto; padding: 0 20px; color: #111; text-align: center; }
    h1 { margin: 0 0 8px; font-size: 22px; }
    p { color: #444; }
    button { margin-top: 20px; padding: 10px 22px; background: #2da44e; color: #fff; border: 0; border-radius: 6px; font: inherit; font-weight: 500; cursor: pointer; }
    .small { font-size: 13px; color: #666; margin-top: 12px; }
  </style>
</head>
<body>
  <h1>Creating "` + escapedName + `" on GitHub…</h1>
  <p>Redirecting to GitHub to register your App.</p>
  <form id="f" action="https://github.com/settings/apps/new" method="post" target="_top">
    <input type="hidden" name="manifest" id="manifest">
    <input type="hidden" name="state" value="` + escapedState + `">
    <button type="submit">Continue to GitHub →</button>
  </form>
  <script>
    document.getElementById("manifest").value = atob(` + string(manifestJSON) + `);
    setTimeout(() => document.getElementById("f").submit(), 250);
  </script>
</body>
</html>`
}

// GitHubSetupPage renders the admin handoff credentials form.
func GitHubSetupPage(token, personaName string) string {
	escapedToken := html.EscapeString(token)
	escapedName := html.EscapeString(personaName)
	tokenJSON, _ := json.Marshal(escapedToken)
	return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>GitHub App setup — ` + escapedName + `</title>
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <style>
    body { font: 15px/1.5 system-ui, sans-serif; max-width: 640px; margin: 40px auto; padding: 0 20px; color: #111; }
    h1 { margin: 0 0 8px; font-size: 22px; }
    p, li { color: #444; }
    label { display: block; font-weight: 600; margin: 16px 0 4px; }
    input, textarea { width: 100%; padding: 8px 10px; border: 1px solid #ccc; border-radius: 6px; font: inherit; box-sizing: border-box; font-family: ui-monospace, monospace; font-size: 12px; }
    textarea { min-height: 120px; }
    button { margin-top: 16px; padding: 10px 16px; background: #111; color: #fff; border: 0; border-radius: 6px; font: inherit; cursor: pointer; }
    button:disabled { opacity: 0.5; cursor: default; }
    .ok { color: #060; margin-top: 12px; }
    .err { color: #b00; margin-top: 12px; }
  </style>
</head>
<body>
  <h1>Install "` + escapedName + `" GitHub App on your org</h1>
  <p>Paste the App ID, private key (.pem), and webhook secret:</p>
  <form id="f">
    <label for="appid">App ID</label>
    <input id="appid" name="appid" required autocomplete="off">
    <label for="pkey">Private key (full PEM)</label>
    <textarea id="pkey" name="pkey" required autocomplete="off"></textarea>
    <label for="whsec">Webhook secret</label>
    <input id="whsec" name="whsec" type="password" required autocomplete="off">
    <button id="submit" type="submit">Continue →</button>
    <p id="msg"></p>
  </form>
  <script>
    const TOKEN = ` + string(tokenJSON) + `;
    document.getElementById("f").addEventListener("submit", async (e) => {
      e.preventDefault();
      const btn = document.getElementById("submit");
      const msg = document.getElementById("msg");
      btn.disabled = true;
      msg.textContent = "Validating with GitHub…";
      msg.className = "";
      try {
        const res = await fetch("/github/publications/credentials", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            formToken: TOKEN,
            appId: document.getElementById("appid").value.trim(),
            privateKey: document.getElementById("pkey").value,
            webhookSecret: document.getElementById("whsec").value,
          }),
        });
        const data = await res.json();
        if (!res.ok) {
          msg.textContent = "Error: " + (data.details || data.error || res.status);
          msg.className = "err";
          btn.disabled = false;
          return;
        }
        msg.textContent = "Redirecting to GitHub…";
        msg.className = "ok";
        window.location.href = data.url;
      } catch (err) {
        msg.textContent = "Network error: " + err.message;
        msg.className = "err";
        btn.disabled = false;
      }
    });
  </script>
</body>
</html>`
}

// SlackSetupPage renders the admin handoff credentials form for Slack.
func SlackSetupPage(token, personaName string) string {
	escapedToken := html.EscapeString(token)
	escapedName := html.EscapeString(personaName)
	tokenJSON, _ := json.Marshal(escapedToken)
	return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Slack App setup — ` + escapedName + `</title>
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <style>
    body { font: 15px/1.5 system-ui, sans-serif; max-width: 640px; margin: 40px auto; padding: 0 20px; color: #111; }
    h1 { margin: 0 0 8px; font-size: 22px; }
    label { display: block; font-weight: 600; margin: 16px 0 4px; }
    input { width: 100%; padding: 8px 10px; border: 1px solid #ccc; border-radius: 6px; font: inherit; box-sizing: border-box; }
    button { margin-top: 16px; padding: 10px 16px; background: #111; color: #fff; border: 0; border-radius: 6px; font: inherit; cursor: pointer; }
    .err { color: #b00; margin-top: 12px; }
    .ok { color: #060; margin-top: 12px; }
  </style>
</head>
<body>
  <h1>Install "` + escapedName + `" Slack App</h1>
  <form id="f">
    <label for="cid">Client ID</label>
    <input id="cid" required autocomplete="off">
    <label for="csec">Client Secret</label>
    <input id="csec" type="password" required autocomplete="off">
    <label for="sign">Signing Secret</label>
    <input id="sign" type="password" required autocomplete="off">
    <button id="submit" type="submit">Continue →</button>
    <p id="msg"></p>
  </form>
  <script>
    const TOKEN = ` + string(tokenJSON) + `;
    document.getElementById("f").addEventListener("submit", async (e) => {
      e.preventDefault();
      const btn = document.getElementById("submit");
      const msg = document.getElementById("msg");
      btn.disabled = true;
      try {
        const res = await fetch("/slack/publications/credentials", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            formToken: TOKEN,
            clientId: document.getElementById("cid").value.trim(),
            clientSecret: document.getElementById("csec").value,
            signingSecret: document.getElementById("sign").value,
          }),
        });
        const data = await res.json();
        if (!res.ok) {
          msg.textContent = "Error: " + (data.details || data.error || res.status);
          msg.className = "err";
          btn.disabled = false;
          return;
        }
        msg.textContent = "Redirecting to Slack…";
        msg.className = "ok";
        window.location.href = data.url;
      } catch (err) {
        msg.textContent = "Network error: " + err.message;
        msg.className = "err";
        btn.disabled = false;
      }
    });
  </script>
</body>
</html>`
}

// GitHubRequestPendingPage is shown when org admin must approve install.
func GitHubRequestPendingPage(setupAction string) string {
	return `<!DOCTYPE html>
<html><body style="font:15px/1.5 system-ui;max-width:560px;margin:40px auto;padding:0 20px">
<h1>Install requested</h1>
<p>The GitHub App install request was sent to an org owner (action: <code>` +
		html.EscapeString(setupAction) + `</code>).
Once approved, GitHub will redirect here again to finish the publish.</p>
</body></html>`
}

// InstallErrorPage renders a simple HTML error for gateway flows.
func InstallErrorPage(message string) string {
	return `<!DOCTYPE html>
<html><body style="font:15px/1.5 system-ui;max-width:560px;margin:40px auto;padding:0 20px">
<h1>Setup error</h1>
<p>` + html.EscapeString(message) + `</p>
</body></html>`
}
