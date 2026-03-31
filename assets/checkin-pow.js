(function () {
  const state = {
    app: "booting",
    info: null,
    powStatus: "",
    busy: false
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
    logoutButton: document.querySelector('[data-role="logout-button"]'),
    powStatus: document.querySelector('[data-role="pow-status"]'),
    powHint: document.querySelector('[data-role="pow-hint"]'),
    lastCheckin: document.querySelector('[data-role="last-checkin"]')
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
    render();

    try {
      const response = await fetch("/api/info", {
        credentials: "same-origin",
        headers: {
          "Accept": "application/json"
        }
      });
      const payload = await response.json();
      state.info = payload;
      state.app = deriveAppState(payload);
    } catch (error) {
      state.info = {
        logged_in: false,
        quota_threshold: 0,
        error: error instanceof Error ? error.message : "加载状态失败，请稍后重试。"
      };
      state.app = "error";
    } finally {
      state.busy = false;
      render();
    }
  }

  async function handleCheckin() {
    if (!state.info || !state.info.logged_in || !state.info.can_checkin) {
      return;
    }

    state.busy = true;
    state.app = "submitting_pow";
    state.powStatus = "";
    render();

    try {
      const pow = state.info.pow || {};
      let payload = "";
      let signature = "";
      let counter = "";
      let hash = "";

      if (pow.enabled) {
        payload = pow.payload || "";
        signature = pow.signature || "";
        counter = "";
        hash = "";
        if (!payload || !signature) {
          throw new Error("PoW 参数不完整，请刷新页面后重试。");
        }

        const solved = await solvePoW(payload, pow.difficulty || 0, pow.expires_at || 0, function (message) {
          state.powStatus = message;
          render();
        });
        counter = String(solved.counter);
        hash = solved.hash;
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
      const payloadState = await response.json();
      state.info = payloadState;
      state.app = deriveAppState(payloadState);
      state.powStatus = "";
    } catch (error) {
      state.info = mergeInfoWithError(state.info, error instanceof Error ? error.message : "签到失败，请稍后重试。");
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
      state.info = await response.json();
      state.app = deriveAppState(state.info);
      state.powStatus = "";
    } catch (error) {
      state.info = mergeInfoWithError(state.info, error instanceof Error ? error.message : "退出登录失败，请稍后重试。");
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
      elements.checkinButton.textContent = state.busy && state.app === "submitting_pow" ? "浏览器验证中..." : "立即签到";
    }

    const pow = info.pow || {};
    toggle(elements.powStatus, Boolean(state.powStatus) && info.logged_in && info.can_checkin);
    setText(elements.powStatus, state.powStatus);
    toggle(elements.powHint, Boolean(pow.enabled) && info.logged_in && info.can_checkin);
    setText(elements.powHint, pow.enabled ? "本次签到需由浏览器完成 PoW 验证，当前难度为 " + (pow.difficulty || 0) + " bit。" : "");

    toggle(elements.lastCheckin, Boolean(info.last_checkin));
    setLastCheckin(elements.lastCheckin, info.last_checkin);
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

  function mergeInfoWithError(info, message) {
    const next = Object.assign({}, info || {});
    next.error = message;
    next.message = "";
    return next;
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
      '</code> 完成签到，本次增加额度 <code>' + escapeHTML(formatQuotaYuan(lastCheckin.quota_awarded || 0)) +
      '</code>（原始值：' + escapeHTML(String(lastCheckin.quota_awarded || 0)) +
      '），额度从 <code>' + escapeHTML(formatQuotaYuan(lastCheckin.quota_before || 0)) +
      '</code> 变为 <code>' + escapeHTML(formatQuotaYuan(lastCheckin.quota_after || 0)) +
      '</code>。';
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
      throw new Error("PoW 挑战已过期，请刷新页面后重试。");
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
