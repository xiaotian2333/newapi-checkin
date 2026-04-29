(function () {
  const state = {
    app: "booting",
    info: null,
    powStatus: "",
    captchaStatus: "",
    busy: false,
    captchaExpanded: false,
    captchaToken: ""
  }

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
    captchaStatus: document.querySelector('[data-role="captcha-status"]'),
    logoutButton: document.querySelector('[data-role="logout-button"]'),
    powStatus: document.querySelector('[data-role="pow-status"]'),
    powHint: document.querySelector('[data-role="pow-hint"]'),
    lastCheckin: document.querySelector('[data-role="last-checkin"]'),
    leaderboardDate: document.querySelector('[data-role="leaderboard-date"]'),
    leaderboardEmpty: document.querySelector('[data-role="leaderboard-empty"]'),
    leaderboardList: document.querySelector('[data-role="leaderboard-list"]')
  }

  if (!elements.loading) {
    return
  }

  if (elements.checkinButton) {
    elements.checkinButton.addEventListener("click", function () {
      if (!state.busy) {
        void handleCheckin()
      }
    })
  }

  if (elements.logoutButton) {
    elements.logoutButton.addEventListener("click", function () {
      if (!state.busy) {
        void handleLogout()
      }
    })
  }

  void refreshInfo()

  async function refreshInfo() {
    state.app = "booting"
    state.busy = true
    state.powStatus = ""
    state.captchaStatus = ""
    render()

    try {
      const response = await fetch("/api/info", {
        credentials: "same-origin",
        headers: {
          "Accept": "application/json"
        }
      })
      applyInfo(await response.json())
    } catch (error) {
      applyInfo({
        logged_in: false,
        quota_threshold: 0,
        error: error instanceof Error ? error.message : "加载状态失败，请稍后重试"
      })
    } finally {
      state.busy = false
      render()
    }
  }

  async function handleCheckin() {
    const info = state.info
    if (!info || !info.logged_in || !info.can_checkin) {
      return
    }

    const captcha = info.captcha || {}
    if (captcha.enabled) {
      if (!state.captchaExpanded) {
        await openCaptchaPanel()
        return
      }
      if (!state.captchaToken) {
        state.app = "awaiting_captcha"
        state.captchaStatus = "请先完成验证码"
        render()
        return
      }
    }

    await startCheckinFlow()
  }

  async function openCaptchaPanel() {
    state.captchaExpanded = true
    state.app = "awaiting_captcha"
    state.captchaStatus = "正在加载验证码..."
    render()

    const captcha = state.info && state.info.captcha ? state.info.captcha : {}
    try {
      await window.CheckinCaptcha.init(
        { type: captcha.type || "cloudflare", siteKey: captcha.site_key },
        handleCaptchaSuccess,
        handleCaptchaFailure
      )
      if (!state.captchaToken) {
        state.captchaStatus = "请完成验证码后继续签到"
      }
    } catch (error) {
      state.captchaStatus = ""
      state.info = mergeInfoWithError(state.info, error instanceof Error ? error.message : "加载验证码失败，请稍后重试")
      state.app = deriveAppState(state.info)
    }
    render()
  }

  function handleCaptchaSuccess(token) {
    state.captchaToken = String(token || "").trim()
    if (!state.captchaToken) {
      state.captchaStatus = "验证码结果无效，请重试"
      render()
      return
    }

    state.captchaStatus = "验证通过，正在获取任务..."
    render()
    if (!state.busy) {
      void startCheckinFlow()
    }
  }

  function handleCaptchaFailure(message) {
    window.CheckinCaptcha.reset()
    state.app = "awaiting_captcha"
    state.captchaStatus = message
    render()
  }

  async function startCheckinFlow() {
    if (!state.info || !state.info.logged_in || !state.info.can_checkin || state.busy) {
      return
    }

    const pow = state.info.pow || {}
    const captcha = state.info.captcha || {}
    const captchaToken = captcha.enabled ? state.captchaToken : ""

    if (captcha.enabled && !captchaToken) {
      state.app = "awaiting_captcha"
      state.captchaStatus = "请先完成验证码"
      render()
      return
    }

    state.busy = true
    state.app = "fetching_pow_task"
    state.powStatus = ""
    if (captcha.enabled) {
      state.captchaStatus = "验证通过，正在获取任务..."
    }
    render()

    try {
      let payload = ""
      let signature = ""
      let counter = ""
      let hash = ""

      if (pow.enabled) {
        state.powStatus = "正在获取 PoW 任务..."
        render()

        let taskResponse
        let taskPayload
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
          })
          taskPayload = await taskResponse.json()
        } finally {
          if (captcha.enabled) {
            window.CheckinCaptcha.reset()
          }
        }

        if (!taskResponse.ok) {
          applyInfo(taskPayload)
          state.powStatus = ""
          state.captchaStatus = captcha.enabled ? "请重新完成验证码后再试" : ""
          return
        }
        if (!taskPayload.enabled) {
          payload = ""
          signature = ""
        } else {
          payload = taskPayload.payload || ""
          signature = taskPayload.signature || ""
          counter = ""
          hash = ""
          if (!payload || !signature) {
            throw new Error("PoW 任务内容不完整，请稍后重试")
          }

          state.app = "submitting_pow"
          state.captchaStatus = ""
          const solved = await window.solvePoW(payload, taskPayload.difficulty || pow.difficulty || 0, taskPayload.expires_at || 0, function (message) {
            state.powStatus = message
            render()
          })
          counter = String(solved.counter)
          hash = solved.hash
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
      })
      applyInfo(await response.json())
      state.powStatus = ""
      state.captchaStatus = ""
    } catch (error) {
      if (captcha.enabled) {
        window.CheckinCaptcha.reset()
        state.captchaStatus = "请重新完成验证码后再试"
      }
      state.info = mergeInfoWithError(state.info, error instanceof Error ? error.message : "签到失败，请稍后重试")
      state.app = deriveAppState(state.info)
    } finally {
      state.busy = false
      render()
    }
  }

  async function handleLogout() {
    state.busy = true
    render()

    try {
      const response = await fetch("/api/logout", {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Accept": "application/json"
        }
      })
      applyInfo(await response.json())
      state.powStatus = ""
    } catch (error) {
      state.info = mergeInfoWithError(state.info, error instanceof Error ? error.message : "退出登录失败，请稍后重试")
      state.app = deriveAppState(state.info)
    } finally {
      state.busy = false
      render()
    }
  }

  function render() {
    const info = state.info || {
      logged_in: false,
      quota_threshold: 0
    }

    toggle(elements.loading, state.app === "booting")
    setText(elements.loading, state.app === "booting" ? "正在加载当前状态..." : "")

    toggle(elements.notice, Boolean(info.message))
    setText(elements.notice, info.message || "")

    toggle(elements.error, Boolean(info.error))
    setText(elements.error, info.error || "")

    toggle(elements.userPanel, Boolean(info.logged_in))
    setText(elements.username, info.username || "-")
    setText(elements.linuxDoId, info.linux_do_id || "-")
    setText(elements.quota, formatQuotaYuan(info.quota || 0))
    setText(elements.quotaThreshold, formatQuotaYuan(info.quota_threshold || 0))

    toggle(elements.loginButton, !info.logged_in)
    toggle(elements.logoutButton, Boolean(info.logged_in))
    if (elements.logoutButton) {
      elements.logoutButton.disabled = state.busy
    }

    toggle(elements.checkinPanel, Boolean(info.logged_in))
    toggle(elements.checkinButton, Boolean(info.logged_in && info.can_checkin))
    toggle(elements.checkinDisabled, Boolean(info.logged_in && !info.can_checkin))
    if (elements.checkinButton) {
      elements.checkinButton.disabled = state.busy
      elements.checkinButton.textContent = state.busy && state.app === "fetching_pow_task"
        ? "获取任务中..."
        : state.busy && state.app === "submitting_pow"
          ? "浏览器验证中..."
          : isCaptchaAwaiting(info)
            ? "等待验证码"
            : "立即签到"
    }
    if (elements.checkinDisabled) {
      elements.checkinDisabled.textContent = getDisabledCheckinText(info)
    }

    const captcha = info.captcha || {}
    const showCaptcha = Boolean(captcha.enabled && info.logged_in && info.can_checkin && state.captchaExpanded)
    toggle(elements.captchaPanel, showCaptcha)
    toggle(elements.captchaStatus, Boolean(state.captchaStatus) && showCaptcha)
    setText(elements.captchaStatus, state.captchaStatus)

    const pow = info.pow || {}
    toggle(elements.powStatus, Boolean(state.powStatus) && info.logged_in && info.can_checkin)
    setText(elements.powStatus, state.powStatus)
    toggle(elements.powHint, Boolean(pow.enabled) && info.logged_in && info.can_checkin)
    setText(elements.powHint, buildPowHintText(pow, captcha))

    toggle(elements.lastCheckin, Boolean(info.last_checkin))
    setLastCheckin(elements.lastCheckin, info.last_checkin)
    renderLeaderboard(info.leaderboard || [], info.leaderboard_date || "")
  }

  function deriveAppState(info) {
    if (!info) {
      return "error"
    }
    if (info.error) {
      return "error"
    }
    if (!info.logged_in) {
      return "anonymous"
    }
    if (info.can_checkin) {
      return "eligible"
    }
    if (info.last_checkin) {
      return "checkin_success"
    }
    return "ineligible"
  }

  function applyInfo(info) {
    state.info = info
    state.app = deriveAppState(info)
    if (!info || !info.logged_in || !info.can_checkin || !((info.captcha || {}).enabled)) {
      resetCaptchaState()
    }
  }

  function mergeInfoWithError(info, message) {
    const next = Object.assign({}, info || {})
    next.error = message
    next.message = ""
    return next
  }

  function isCaptchaAwaiting(info) {
    const captcha = info.captcha || {}
    return Boolean(captcha.enabled && state.captchaExpanded && !state.captchaToken && !state.busy)
  }

  function buildPowHintText(pow, captcha) {
    if (!pow.enabled) {
      return ""
    }
    if (captcha.enabled) {
      return "完成验证码后将继续执行浏览器 PoW，当前难度为 " + (pow.difficulty || 0) + " bit"
    }
    return "本次签到需由浏览器完成 PoW 验证，当前难度为 " + (pow.difficulty || 0) + " bit"
  }

  function resetCaptchaState() {
    state.captchaExpanded = false
    state.captchaStatus = ""
    window.CheckinCaptcha.reset()
  }

  function getDisabledCheckinText(info) {
    if (info.last_checkin) {
      return "明天再来签到吧"
    }
    if (Number(info.quota || 0) >= Number(info.quota_threshold || 0)) {
      return "当前额度充足，无需签到"
    }
    return "明天再来签到吧"
  }

  function toggle(element, visible) {
    if (!element) {
      return
    }
    element.classList.toggle("hidden", !visible)
  }

  function setText(element, value) {
    if (!element) {
      return
    }
    element.textContent = value
  }

  function setLastCheckin(element, lastCheckin) {
    if (!element) {
      return
    }
    if (!lastCheckin) {
      element.innerHTML = ""
      return
    }

    element.innerHTML =
      '最近签到结果：用户 <code>' + escapeHTML(String(lastCheckin.user_id)) +
      '</code> 于 <code>' + escapeHTML(String(lastCheckin.checkin_date)) +
      '</code> 完成签到，本次增加额度 <code>' + escapeHTML(formatQuotaYuan(lastCheckin.quota_awarded || 0)) + '</code>，额度从 <code>' + escapeHTML(formatQuotaYuan(lastCheckin.quota_before || 0)) +
      '</code> 变为 <code>' + escapeHTML(formatQuotaYuan(lastCheckin.quota_after || 0)) +
      '</code>'
  }

  function renderLeaderboard(items, checkinDate) {
    if (elements.leaderboardDate) {
      elements.leaderboardDate.textContent = "统计日期：" + (checkinDate || "-")
    }
    if (!elements.leaderboardList || !elements.leaderboardEmpty) {
      return
    }

    elements.leaderboardList.replaceChildren()
    toggle(elements.leaderboardEmpty, !items.length)
    toggle(elements.leaderboardList, Boolean(items.length))
    if (!items.length) {
      return
    }

    items.forEach(function (item) {
      const listItem = document.createElement("li")
      listItem.className = "leaderboard-item"

      const rank = document.createElement("div")
      rank.className = "leaderboard-rank"
      rank.textContent = String(item.rank || 0)
      listItem.appendChild(rank)

      const main = document.createElement("div")
      main.className = "leaderboard-main"

      const topLine = document.createElement("div")
      topLine.className = "leaderboard-topline"

      const name = document.createElement("div")
      name.className = "leaderboard-name"

      const medal = getLeaderboardMedal(item.rank || 0)
      if (medal) {
        const medalNode = document.createElement("span")
        medalNode.className = "leaderboard-medal"
        medalNode.setAttribute("aria-hidden", "true")
        medalNode.textContent = medal
        name.appendChild(medalNode)
      }

      const nameText = document.createElement("span")
      nameText.className = "leaderboard-name-text"
      nameText.textContent = item.username || ("用户 " + String(item.user_id || 0))
      name.appendChild(nameText)
      topLine.appendChild(name)

      const award = document.createElement("div")
      award.className = "leaderboard-award"
      award.textContent = formatQuotaYuan(item.quota_awarded || 0)
      topLine.appendChild(award)
      main.appendChild(topLine)

      const meta = document.createElement("div")
      meta.className = "leaderboard-meta"
      meta.textContent = "签到时间 " + formatCheckinTime(item.created_at || 0)
      main.appendChild(meta)

      listItem.appendChild(main)
      elements.leaderboardList.appendChild(listItem)
    })
  }

  function getLeaderboardMedal(rank) {
    if (rank === 1) {
      return "\u{1F947}"
    }
    if (rank === 2) {
      return "\u{1F948}"
    }
    if (rank === 3) {
      return "\u{1F949}"
    }
    return ""
  }

  function formatCheckinTime(value) {
    if (!value) {
      return "-"
    }

    const date = new Date(Number(value) * 1000)
    if (Number.isNaN(date.getTime())) {
      return "-"
    }

    return [date.getHours(), date.getMinutes(), date.getSeconds()]
      .map(function (part) {
        return String(part).padStart(2, "0")
      })
      .join(":")
  }

  function escapeHTML(value) {
    return value
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#39;")
  }

  function formatQuotaYuan(value) {
    const rate = 500000
    const sign = value < 0 ? "-" : ""
    const absolute = Math.abs(value)
    const yuan = Math.floor(absolute / rate)
    const fraction = Math.floor((absolute % rate) * 100 / rate)
    if (fraction === 0) {
      return sign + "￥" + yuan
    }
    const decimal = String(fraction).padStart(2, "0").replace(/0+$/, "")
    return sign + "￥" + yuan + "." + decimal
  }
})()
