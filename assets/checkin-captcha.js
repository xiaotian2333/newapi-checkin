(function () {
  const captchaScriptURLs = {
    cloudflare: "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit",
    hcaptcha: "https://js.hcaptcha.com/1/api.js?render=explicit"
  }

  let widgetId = null
  let scriptPromise = null
  let captchaType = "cloudflare"
  let onSuccess = null
  let onError = null

  function getElement() {
    return document.querySelector('[data-role="captcha-widget"]')
  }

  async function ensureScript(type) {
    if (type === "hcaptcha") {
      if (window.hcaptcha && typeof window.hcaptcha.render === "function") {
        return
      }
    } else {
      if (window.turnstile && typeof window.turnstile.render === "function") {
        return
      }
    }
    if (scriptPromise) {
      return scriptPromise
    }

    scriptPromise = new Promise(function (resolve, reject) {
      const script = document.createElement("script")
      script.src = captchaScriptURLs[type] || captchaScriptURLs.cloudflare
      script.async = true
      script.defer = true
      script.setAttribute("data-role", "captcha-api")
      script.addEventListener("load", function () {
        const apiReady = type === "hcaptcha"
          ? window.hcaptcha && typeof window.hcaptcha.render === "function"
          : window.turnstile && typeof window.turnstile.render === "function"
        if (apiReady) {
          resolve()
          return
        }
        reject(new Error("验证码脚本加载失败，请稍后重试"))
      })
      script.addEventListener("error", function () {
        reject(new Error("验证码脚本加载失败，请稍后重试"))
      })
      document.head.appendChild(script)
    }).catch(function (error) {
      scriptPromise = null
      throw error
    })

    return scriptPromise
  }

  function renderWidget(config) {
    const element = getElement()
    if (!element) {
      throw new Error("验证码容器不存在")
    }

    if (config.type === "hcaptcha") {
      widgetId = window.hcaptcha.render(element, {
        sitekey: config.siteKey,
        theme: "auto",
        size: "normal",
        callback: function (token) {
          if (onSuccess) {
            onSuccess(token)
          }
        },
        "error-callback": function () {
          if (onError) {
            onError("验证码加载失败，请稍后重试")
          }
        },
        "expired-callback": function () {
          if (onError) {
            onError("验证码已过期，请重新验证")
          }
        },
        "chalexpired-callback": function () {
          if (onError) {
            onError("验证码校验超时，请重新验证")
          }
        }
      })
    } else {
      widgetId = window.turnstile.render(element, {
        sitekey: config.siteKey,
        action: "checkin",
        theme: "auto",
        size: "flexible",
        callback: function (token) {
          if (onSuccess) {
            onSuccess(token)
          }
        },
        "error-callback": function () {
          if (onError) {
            onError("验证码加载失败，请稍后重试")
          }
        },
        "expired-callback": function () {
          if (onError) {
            onError("验证码已过期，请重新验证")
          }
        },
        "timeout-callback": function () {
          if (onError) {
            onError("验证码校验超时，请重新验证")
          }
        },
        "response-field": false
      })
    }
  }

  async function init(config, _onSuccess, _onError) {
    captchaType = config.type || "cloudflare"
    onSuccess = _onSuccess
    onError = _onError

    await ensureScript(captchaType)
    if (widgetId !== null) {
      return
    }
    renderWidget(config)
  }

  function reset() {
    if (widgetId === null) {
      return
    }
    if (captchaType === "hcaptcha") {
      if (!window.hcaptcha || typeof window.hcaptcha.reset !== "function") {
        return
      }
    } else {
      if (!window.turnstile || typeof window.turnstile.reset !== "function") {
        return
      }
    }
    try {
      if (captchaType === "hcaptcha") {
        window.hcaptcha.reset(widgetId)
      } else {
        window.turnstile.reset(widgetId)
      }
    } catch (e) {
      // ignore reset errors
    }
  }

  window.CheckinCaptcha = {
    init: init,
    reset: reset
  }
})()
