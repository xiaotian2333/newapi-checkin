(function () {
  const turnstileScriptURL = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";
  const state = {
    app: "booting",
    info: null,
    powStatus: "",
    captchaStatus: "",
    busy: false,
    captchaExpanded: false,
    captchaToken: "",
    captchaWidgetId: null,
    captchaScriptPromise: null
  };

  const elements = {
    loading: document.querySelector('[data-role="loading"]'),
    notice: document.querySelector('[data-role="notice"]'),
    error: document.querySelector('[data-role="error"]'),
    userPanel: document.querySelector('[data-role="user-panel"]'),
    username: document.querySelector('[data-role="username"]'),
    linuxDoId: document.querySelector('[data-role="linux-do-id"]'),
    quota: document.querySelector('[data-role="quota"]'),
    quotaThreshold: document.querySelector('[data-role="quota-threshold"]'),
    loginButton: document.querySelector('[data-role="login-button"]'),
    checkinPanel: document.querySelector('[data-role="checkin-panel"]'),
    checkinButton: document.querySelector('[data-role="checkin-button"]'),
    checkinDisabled: document.querySelector('[data-role="checkin-disabled"]'),
    captchaPanel: document.querySelector('[data-role="captcha-panel"]'),
    captchaWidget: document.querySelector('[data-role="captcha-widget"]'),
    captchaStatus: document.querySelector('[data-role="captcha-status"]'),
    logoutButton: document.querySelector('[data-role="logout-button"]'),
    powStatus: document.querySelector('[data-role="pow-status"]'),
    powHint: document.querySelector('[data-role="pow-hint"]'),
    lastCheckin: document.querySelector('[data-role="last-checkin"]'),
    leaderboardDate: document.querySelector('[data-role="leaderboard-date"]'),
    leaderboardEmpty: document.querySelector('[data-role="leaderboard-empty"]'),
    leaderboardList: document.querySelector('[data-role="leaderboard-list"]')
  };

  if (!elements.loading) {
    return;
  }

  if (elements.checkinButton) {
    elements.checkinButton.addEventListener("click", function () {
      if (!state.busy) {
        void handleCheckin();
      }
    });
  }

  if (elements.logoutButton) {
    elements.logoutButton.addEventListener("click", function () {
      if (!state.busy) {
        void handleLogout();
      }
    });
  }

  void refreshInfo();

  async function refreshInfo() {
    state.app = "booting";
    state.busy = true;
    state.powStatus = "";
    state.captchaStatus = "";
    render();

    try {
      const response = await fetch("/api/info", {
        credentials: "same-origin",
        headers: {
          "Accept": "application/json"
        }
      });
      applyInfo(await response.json());
    } catch (error) {
      applyInfo({
        logged_in: false,
        quota_threshold: 0,
        error: error instanceof Error ? error.message : "加载状态失败，请稍后重试"
      });
    } finally {
      state.busy = false;
      render();
    }
  }

  async function handleCheckin() {
    const info = state.info;
    if (!info || !info.logged_in || !info.can_checkin) {
      return;
    }

    const captcha = info.captcha || {};
    if (captcha.enabled) {
      if (!state.captchaExpanded) {
        await openCaptchaPanel();
        return;
      }
      if (!state.captchaToken) {
        state.app = "awaiting_captcha";
        state.captchaStatus = "请先完成验证码";
        render();
        return;
      }
    }

    await startCheckinFlow();
  }

  async function openCaptchaPanel() {
    state.captchaExpanded = true;
    state.app = "awaiting_captcha";
    state.captchaStatus = "正在加载验证码...";
    render();

    try {
      await ensureCaptchaWidget();
      if (!state.captchaToken) {
        state.captchaStatus = "请完成验证码后继续签到";
      }
    } catch (error) {
      state.captchaStatus = "";
      state.info = mergeInfoWithError(state.info, error instanceof Error ? error.message : "加载验证码失败，请稍后重试");
      state.app = deriveAppState(state.info);
    }
    render();
  }

  async function ensureCaptchaWidget() {
    const captcha = state.info && state.info.captcha ? state.info.captcha : {};
    if (!captcha.enabled) {
      return;
    }

    await ensureTurnstileScript();
    if (state.captchaWidgetId !== null) {
      return;
    }
    if (!elements.captchaWidget) {
      throw new Error("验证码容器不存在");
    }

    state.captchaWidgetId = window.turnstile.render(elements.captchaWidget, {
      sitekey: captcha.site_key,
      action: "checkin",
      theme: "auto",
      size: "flexible",
      callback: function (token) {
        handleCaptchaSuccess(token);
      },
      "error-callback": function () {
        handleCaptchaFailure("验证码加载失败，请稍后重试");
      },
      "expired-callback": function () {
        handleCaptchaFailure("验证码已过期，请重新验证");
      },
      "timeout-callback": function () {
        handleCaptchaFailure("验证码校验超时，请重新验证");
      },
      "response-field": false
    });
  }

  async function ensureTurnstileScript() {
    if (window.turnstile && typeof window.turnstile.render === "function") {
      return;
    }
    if (state.captchaScriptPromise) {
      return state.captchaScriptPromise;
    }

    state.captchaScriptPromise = new Promise(function (resolve, reject) {
      const script = document.createElement("script");
      script.src = turnstileScriptURL;
      script.async = true;
      script.defer = true;
      script.setAttribute("data-role", "turnstile-api");
      script.addEventListener("load", function () {
        if (window.turnstile && typeof window.turnstile.render === "function") {
          resolve();
          return;
        }
        reject(new Error("验证码脚本加载失败，请稍后重试"));
      });
      script.addEventListener("error", function () {
        reject(new Error("验证码脚本加载失败，请稍后重试"));
      });
      document.head.appendChild(script);
    }).catch(function (error) {
      state.captchaScriptPromise = null;
      throw error;
    });

    return state.captchaScriptPromise;
  }

  function handleCaptchaSuccess(token) {
    state.captchaToken = String(token || "").trim();
    if (!state.captchaToken) {
      state.captchaStatus = "验证码结果无效，请重试";
      render();
      return;
    }

    state.captchaStatus = "验证通过，正在获取任务...";
    render();
    if (!state.busy) {
      void startCheckinFlow();
    }
  }

  function handleCaptchaFailure(message) {
    resetCaptchaChallenge();
    state.app = "awaiting_captcha";
    state.captchaStatus = message;
    render();
  }

  async function startCheckinFlow() {
    if (!state.info || !state.info.logged_in || !state.info.can_checkin || state.busy) {
      return;
    }

    const pow = state.info.pow || {};
    const captcha = state.info.captcha || {};
    const captchaToken = captcha.enabled ? state.captchaToken : "";

    if (captcha.enabled && !captchaToken) {
      state.app = "awaiting_captcha";
      state.captchaStatus = "请先完成验证码";
      render();
      return;
    }

    state.busy = true;
    state.app = "fetching_pow_task";
    state.powStatus = "";
    if (captcha.enabled) {
      state.captchaStatus = "验证通过，正在获取任务...";
    }
    render();

    try {
      let payload = "";
      let signature = "";
      let counter = "";
      let hash = "";

      if (pow.enabled) {
        state.powStatus = "正在获取 PoW 任务...";
        render();

        let taskResponse;
        let taskPayload;
        try {
          taskResponse = await fetch("/api/checkin/task", {
            method: "POST",
            credentials: "same-origin",
            headers: {
              "Accept": "application/json",
              "Content-Type": "application/json"
            },
            body: JSON.stringify({
              captcha_token: captchaToken
            })
          });
          taskPayload = await taskResponse.json();
        } finally {
          if (captcha.enabled) {
            resetCaptchaChallenge();
          }
        }

        if (!taskResponse.ok) {
          applyInfo(taskPayload);
          state.powStatus = "";
          state.captchaStatus = captcha.enabled ? "请重新完成验证码后再试" : "";
          return;
        }
        if (!taskPayload.enabled) {
          payload = "";
          signature = "";
        } else {
          payload = taskPayload.payload || "";
          signature = taskPayload.signature || "";
          counter = "";
          hash = "";
          if (!payload || !signature) {
            throw new Error("PoW 任务内容不完整，请稍后重试");
          }

          state.app = "submitting_pow";
          state.captchaStatus = "";
          const solved = await solvePoW(payload, taskPayload.difficulty || pow.difficulty || 0, taskPayload.expires_at || 0, function (message) {
            state.powStatus = message;
            render();
          });
          counter = String(solved.counter);
          hash = solved.hash;
        }
      }

      const response = await fetch("/api/checkin", {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Accept": "application/json",
          "Content-Type": "application/json"
        },
        body: JSON.stringify({
          pow_payload: payload,
          pow_signature: signature,
          pow_counter: counter,
          pow_hash: hash
        })
      });
      applyInfo(await response.json());
      state.powStatus = "";
      state.captchaStatus = "";
    } catch (error) {
      if (captcha.enabled) {
        resetCaptchaChallenge();
        state.captchaStatus = "请重新完成验证码后再试";
      }
      state.info = mergeInfoWithError(state.info, error instanceof Error ? error.message : "签到失败，请稍后重试");
      state.app = deriveAppState(state.info);
    } finally {
      state.busy = false;
      render();
    }
  }

  async function handleLogout() {
    state.busy = true;
    render();

    try {
      const response = await fetch("/api/logout", {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Accept": "application/json"
        }
      });
      applyInfo(await response.json());
      state.powStatus = "";
    } catch (error) {
      state.info = mergeInfoWithError(state.info, error instanceof Error ? error.message : "退出登录失败，请稍后重试");
      state.app = deriveAppState(state.info);
    } finally {
      state.busy = false;
      render();
    }
  }

  function render() {
    const info = state.info || {
      logged_in: false,
      quota_threshold: 0
    };

    toggle(elements.loading, state.app === "booting");
    setText(elements.loading, state.app === "booting" ? "正在加载当前状态..." : "");

    toggle(elements.notice, Boolean(info.message));
    setText(elements.notice, info.message || "");

    toggle(elements.error, Boolean(info.error));
    setText(elements.error, info.error || "");

    toggle(elements.userPanel, Boolean(info.logged_in));
    setText(elements.username, info.username || "-");
    setText(elements.linuxDoId, info.linux_do_id || "-");
    setText(elements.quota, formatQuotaYuan(info.quota || 0));
    setText(elements.quotaThreshold, formatQuotaYuan(info.quota_threshold || 0));

    toggle(elements.loginButton, !info.logged_in);
    toggle(elements.logoutButton, Boolean(info.logged_in));
    if (elements.logoutButton) {
      elements.logoutButton.disabled = state.busy;
    }

    toggle(elements.checkinPanel, Boolean(info.logged_in));
    toggle(elements.checkinButton, Boolean(info.logged_in && info.can_checkin));
    toggle(elements.checkinDisabled, Boolean(info.logged_in && !info.can_checkin));
    if (elements.checkinButton) {
      elements.checkinButton.disabled = state.busy;
      elements.checkinButton.textContent = state.busy && state.app === "fetching_pow_task"
        ? "获取任务中..."
        : state.busy && state.app === "submitting_pow"
          ? "浏览器验证中..."
          : isCaptchaAwaiting(info)
            ? "等待验证码"
            : "立即签到";
    }
    if (elements.checkinDisabled) {
      elements.checkinDisabled.textContent = getDisabledCheckinText(info);
    }

    const captcha = info.captcha || {};
    const showCaptcha = Boolean(captcha.enabled && info.logged_in && info.can_checkin && state.captchaExpanded);
    toggle(elements.captchaPanel, showCaptcha);
    toggle(elements.captchaStatus, Boolean(state.captchaStatus) && showCaptcha);
    setText(elements.captchaStatus, state.captchaStatus);

    const pow = info.pow || {};
    toggle(elements.powStatus, Boolean(state.powStatus) && info.logged_in && info.can_checkin);
    setText(elements.powStatus, state.powStatus);
    toggle(elements.powHint, Boolean(pow.enabled) && info.logged_in && info.can_checkin);
    setText(elements.powHint, buildPowHintText(pow, captcha));

    toggle(elements.lastCheckin, Boolean(info.last_checkin));
    setLastCheckin(elements.lastCheckin, info.last_checkin);
    renderLeaderboard(info.leaderboard || [], info.leaderboard_date || "");
  }

  function deriveAppState(info) {
    if (!info) {
      return "error";
    }
    if (info.error) {
      return "error";
    }
    if (!info.logged_in) {
      return "anonymous";
    }
    if (info.can_checkin) {
      return "eligible";
    }
    if (info.last_checkin) {
      return "checkin_success";
    }
    return "ineligible";
  }

  function applyInfo(info) {
    state.info = info;
    state.app = deriveAppState(info);
    if (!info || !info.logged_in || !info.can_checkin || !((info.captcha || {}).enabled)) {
      resetCaptchaState();
    }
  }

  function mergeInfoWithError(info, message) {
    const next = Object.assign({}, info || {});
    next.error = message;
    next.message = "";
    return next;
  }

  function isCaptchaAwaiting(info) {
    const captcha = info.captcha || {};
    return Boolean(captcha.enabled && state.captchaExpanded && !state.captchaToken && !state.busy);
  }

  function buildPowHintText(pow, captcha) {
    if (!pow.enabled) {
      return "";
    }
    if (captcha.enabled) {
      return "完成验证码后将继续执行浏览器 PoW，当前难度为 " + (pow.difficulty || 0) + " bit";
    }
    return "本次签到需由浏览器完成 PoW 验证，当前难度为 " + (pow.difficulty || 0) + " bit";
  }

  function resetCaptchaState() {
    state.captchaExpanded = false;
    state.captchaStatus = "";
    resetCaptchaChallenge();
  }

  function resetCaptchaChallenge() {
    state.captchaToken = "";
    if (state.captchaWidgetId === null) {
      return;
    }
    if (!window.turnstile || typeof window.turnstile.reset !== "function") {
      return;
    }
    try {
      window.turnstile.reset(state.captchaWidgetId);
    } catch (error) {
    }
  }

  function getDisabledCheckinText(info) {
    if (info.last_checkin) {
      return "明天再来签到吧";
    }
    if (Number(info.quota || 0) >= Number(info.quota_threshold || 0)) {
      return "当前额度充足，无需签到";
    }
    return "明天再来签到吧";
  }

  function toggle(element, visible) {
    if (!element) {
      return;
    }
    element.classList.toggle("hidden", !visible);
  }

  function setText(element, value) {
    if (!element) {
      return;
    }
    element.textContent = value;
  }

  function setLastCheckin(element, lastCheckin) {
    if (!element) {
      return;
    }
    if (!lastCheckin) {
      element.innerHTML = "";
      return;
    }

    element.innerHTML =
      '最近签到结果：用户 <code>' + escapeHTML(String(lastCheckin.user_id)) +
      '</code> 于 <code>' + escapeHTML(String(lastCheckin.checkin_date)) +
      '</code> 完成签到，本次增加额度 <code>' + escapeHTML(formatQuotaYuan(lastCheckin.quota_awarded || 0)) + '</code>，额度从 <code>' + escapeHTML(formatQuotaYuan(lastCheckin.quota_before || 0)) +
      '</code> 变为 <code>' + escapeHTML(formatQuotaYuan(lastCheckin.quota_after || 0)) +
      '</code>';
  }

  function renderLeaderboard(items, checkinDate) {
    if (elements.leaderboardDate) {
      elements.leaderboardDate.textContent = "统计日期：" + (checkinDate || "-");
    }
    if (!elements.leaderboardList || !elements.leaderboardEmpty) {
      return;
    }

    elements.leaderboardList.replaceChildren();
    toggle(elements.leaderboardEmpty, !items.length);
    toggle(elements.leaderboardList, Boolean(items.length));
    if (!items.length) {
      return;
    }

    items.forEach(function (item) {
      const listItem = document.createElement("li");
      listItem.className = "leaderboard-item";

      const rank = document.createElement("div");
      rank.className = "leaderboard-rank";
      rank.textContent = String(item.rank || 0);
      listItem.appendChild(rank);

      const main = document.createElement("div");
      main.className = "leaderboard-main";

      const topLine = document.createElement("div");
      topLine.className = "leaderboard-topline";

      const name = document.createElement("div");
      name.className = "leaderboard-name";

      const medal = getLeaderboardMedal(item.rank || 0);
      if (medal) {
        const medalNode = document.createElement("span");
        medalNode.className = "leaderboard-medal";
        medalNode.setAttribute("aria-hidden", "true");
        medalNode.textContent = medal;
        name.appendChild(medalNode);
      }

      const nameText = document.createElement("span");
      nameText.className = "leaderboard-name-text";
      nameText.textContent = item.username || ("用户 " + String(item.user_id || 0));
      name.appendChild(nameText);
      topLine.appendChild(name);

      const award = document.createElement("div");
      award.className = "leaderboard-award";
      award.textContent = formatQuotaYuan(item.quota_awarded || 0);
      topLine.appendChild(award);
      main.appendChild(topLine);

      const meta = document.createElement("div");
      meta.className = "leaderboard-meta";
      meta.textContent = "签到时间 " + formatCheckinTime(item.created_at || 0);
      main.appendChild(meta);

      listItem.appendChild(main);
      elements.leaderboardList.appendChild(listItem);
    });
  }

  function getLeaderboardMedal(rank) {
    if (rank === 1) {
      return "🥇";
    }
    if (rank === 2) {
      return "🥈";
    }
    if (rank === 3) {
      return "🥉";
    }
    return "";
  }

  function formatCheckinTime(value) {
    if (!value) {
      return "-";
    }

    const date = new Date(Number(value) * 1000);
    if (Number.isNaN(date.getTime())) {
      return "-";
    }

    return [date.getHours(), date.getMinutes(), date.getSeconds()]
      .map(function (part) {
        return String(part).padStart(2, "0");
      })
      .join(":");
  }

  function escapeHTML(value) {
    return value
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#39;");
  }

  function formatQuotaYuan(value) {
    const rate = 500000;
    const sign = value < 0 ? "-" : "";
    const absolute = Math.abs(value);
    const yuan = Math.floor(absolute / rate);
    const fraction = Math.floor((absolute % rate) * 100 / rate);
    if (fraction === 0) {
      return sign + "￥" + yuan;
    }
    const decimal = String(fraction).padStart(2, "0").replace(/0+$/, "");
    return sign + "￥" + yuan + "." + decimal;
  }
})();

async function solvePoW(payload, difficulty, expiresAt, reportStatus) {
  let counter = 0;
  const batchSize = 2000;

  while (true) {
    if (Date.now() >= expiresAt * 1000) {
      throw new Error("PoW 挑战已过期，请刷新页面后重试");
    }

    const batchEnd = counter + batchSize;
    for (; counter < batchEnd; counter += 1) {
      const hash = sha256Hex(payload + ":" + counter);
      if (hasLeadingZeroBits(hash, difficulty)) {
        return { counter, hash };
      }
    }

    reportStatus("正在执行 PoW 计算，已尝试 " + counter + " 次...");
    await pauseForFrame();
  }
}

function pauseForFrame() {
  return new Promise(function (resolve) {
    window.setTimeout(resolve, 0);
  });
}

function hasLeadingZeroBits(hash, bits) {
  if (bits <= 0) {
    return true;
  }

  let remaining = bits;
  for (let index = 0; index < hash.length; index += 2) {
    const byteValue = Number.parseInt(hash.slice(index, index + 2), 16);
    if (Number.isNaN(byteValue)) {
      return false;
    }
    if (remaining >= 8) {
      if (byteValue !== 0) {
        return false;
      }
      remaining -= 8;
      continue;
    }
    return (byteValue >> (8 - remaining)) === 0;
  }

  return remaining <= 0;
}

function sha256Hex(input) {
  const message = new TextEncoder().encode(input);
  const bitLength = BigInt(message.length) * 8n;
  const totalLength = ((message.length + 9 + 63) >> 6) << 6;
  const padded = new Uint8Array(totalLength);
  padded.set(message);
  padded[message.length] = 0x80;

  const view = new DataView(padded.buffer);
  view.setUint32(totalLength - 8, Number((bitLength >> 32n) & 0xffffffffn));
  view.setUint32(totalLength - 4, Number(bitLength & 0xffffffffn));

  let h0 = 0x6a09e667;
  let h1 = 0xbb67ae85;
  let h2 = 0x3c6ef372;
  let h3 = 0xa54ff53a;
  let h4 = 0x510e527f;
  let h5 = 0x9b05688c;
  let h6 = 0x1f83d9ab;
  let h7 = 0x5be0cd19;

  const schedule = new Uint32Array(64);
  for (let offset = 0; offset < totalLength; offset += 64) {
    for (let index = 0; index < 16; index += 1) {
      schedule[index] = view.getUint32(offset + index * 4);
    }
    for (let index = 16; index < 64; index += 1) {
      const s0 = rotateRight(schedule[index - 15], 7) ^ rotateRight(schedule[index - 15], 18) ^ (schedule[index - 15] >>> 3);
      const s1 = rotateRight(schedule[index - 2], 17) ^ rotateRight(schedule[index - 2], 19) ^ (schedule[index - 2] >>> 10);
      schedule[index] = (schedule[index - 16] + s0 + schedule[index - 7] + s1) >>> 0;
    }

    let a = h0;
    let b = h1;
    let c = h2;
    let d = h3;
    let e = h4;
    let f = h5;
    let g = h6;
    let h = h7;

    for (let index = 0; index < 64; index += 1) {
      const s1 = rotateRight(e, 6) ^ rotateRight(e, 11) ^ rotateRight(e, 25);
      const choose = (e & f) ^ (~e & g);
      const temp1 = (h + s1 + choose + SHA256_K[index] + schedule[index]) >>> 0;
      const s0 = rotateRight(a, 2) ^ rotateRight(a, 13) ^ rotateRight(a, 22);
      const majority = (a & b) ^ (a & c) ^ (b & c);
      const temp2 = (s0 + majority) >>> 0;

      h = g;
      g = f;
      f = e;
      e = (d + temp1) >>> 0;
      d = c;
      c = b;
      b = a;
      a = (temp1 + temp2) >>> 0;
    }

    h0 = (h0 + a) >>> 0;
    h1 = (h1 + b) >>> 0;
    h2 = (h2 + c) >>> 0;
    h3 = (h3 + d) >>> 0;
    h4 = (h4 + e) >>> 0;
    h5 = (h5 + f) >>> 0;
    h6 = (h6 + g) >>> 0;
    h7 = (h7 + h) >>> 0;
  }

  return [h0, h1, h2, h3, h4, h5, h6, h7]
    .map(function (value) {
      return value.toString(16).padStart(8, "0");
    })
    .join("");
}

function rotateRight(value, shift) {
  return (value >>> shift) | (value << (32 - shift));
}

const SHA256_K = [
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
  0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
  0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
  0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
  0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
  0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
  0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
  0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
];
