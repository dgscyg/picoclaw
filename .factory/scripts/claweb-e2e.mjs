import process from "node:process";

function parseArgs(argv) {
  const args = {
    base: "http://127.0.0.1:18081",
    passphrase: "change-me",
    clientId: "validator-client",
    userId: "user-guest-a",
    roomId: "",
    message: "reply with exactly pong",
    timeoutMs: 90000,
  };

  for (let i = 2; i < argv.length; i += 1) {
    const a = argv[i];
    if (a === "--base") args.base = argv[++i] || args.base;
    else if (a === "--passphrase") args.passphrase = argv[++i] || args.passphrase;
    else if (a === "--clientId") args.clientId = argv[++i] || args.clientId;
    else if (a === "--userId") args.userId = argv[++i] || args.userId;
    else if (a === "--roomId") args.roomId = argv[++i] || args.roomId;
    else if (a === "--message") args.message = argv[++i] || args.message;
    else if (a === "--timeoutMs") args.timeoutMs = Number(argv[++i] || args.timeoutMs);
  }

  return args;
}

function assert(cond, msg) {
  if (!cond) throw new Error(msg);
}

async function login(base, passphrase) {
  const res = await fetch(`${base.replace(/\/$/, "")}/login`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ passphrase }),
  });

  assert(res.ok, `login failed with status ${res.status}`);
  const json = await res.json();
  assert(json?.ok === true, `login response not ok: ${JSON.stringify(json).slice(0, 300)}`);
  assert(typeof json?.session?.token === "string" && json.session.token.length > 5, "missing session token");
  assert(typeof json?.session?.wsUrl === "string" && json.session.wsUrl.startsWith("/"), "missing wsUrl");

  return { token: json.session.token, wsUrl: json.session.wsUrl };
}

async function main() {
  const args = parseArgs(process.argv);
  const { token, wsUrl } = await login(args.base, args.passphrase);
  const baseUrl = new URL(args.base);
  const wsScheme = baseUrl.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(`${wsScheme}//${baseUrl.host}${wsUrl}`);
  const turnId = `turn-${Date.now()}`;

  await new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      ws.close();
      reject(new Error("claweb e2e timed out"));
    }, args.timeoutMs);

    let ready = false;

    ws.addEventListener("open", () => {
      ws.send(JSON.stringify({
        type: "hello",
        token,
        clientId: args.clientId,
        userId: args.userId,
        roomId: args.roomId || undefined,
      }));
    });

    ws.addEventListener("message", (event) => {
      let frame;
      try {
        frame = JSON.parse(String(event.data));
      } catch (err) {
        clearTimeout(timeout);
        ws.close();
        reject(new Error(`invalid JSON frame: ${err.message}`));
        return;
      }

      if (frame.type === "ready") {
        ready = true;
        ws.send(JSON.stringify({ type: "message", id: turnId, text: args.message }));
        return;
      }

      if (frame.type === "error") {
        clearTimeout(timeout);
        ws.close();
        reject(new Error(`claweb error frame: ${frame.message || "unknown"}`));
        return;
      }

      if (ready && frame.type === "message" && typeof frame.text === "string" && frame.text.trim() !== "") {
        clearTimeout(timeout);
        console.log(frame.text);
        ws.close();
        resolve();
      }
    });

    ws.addEventListener("error", (event) => {
      clearTimeout(timeout);
      ws.close();
      reject(new Error(`websocket error: ${event.message || "unknown"}`));
    });
  });
}

main().catch((err) => {
  console.error(`[claweb-e2e] FAIL: ${err.stack || err.message}`);
  process.exit(1);
});
