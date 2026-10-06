/* vanDyke UI: theme, dialogs, toasts and Umami analytics events.
   Loaded synchronously in <head> so the colour theme never flashes. */
(function () {
  "use strict";

  /* ---------- theme ---------- */
  var STORAGE_KEY = "vandyke-theme";
  var mq = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;
  var stored = null;
  try {
    stored = window.localStorage.getItem(STORAGE_KEY);
  } catch (err) {
    /* storage unavailable (private mode) — follow the system */
  }

  function resolvedTheme() {
    if (stored === "dark" || stored === "light") return stored;
    return mq && mq.matches ? "dark" : "light";
  }

  function applyTheme(value) {
    document.documentElement.setAttribute("data-theme", value);
  }

  applyTheme(resolvedTheme());

  if (mq) {
    var onSystemChange = function () {
      if (stored !== "dark" && stored !== "light") applyTheme(mq.matches ? "dark" : "light");
    };
    if (mq.addEventListener) mq.addEventListener("change", onSystemChange);
    else if (mq.addListener) mq.addListener(onSystemChange);
  }

  /* ---------- analytics (never allowed to break the page) ---------- */
  function track(name, data) {
    if (window.umami && typeof window.umami.track === "function") {
      try {
        window.umami.track(name, data || {});
      } catch (err) {
        /* ignore */
      }
    }
  }

  /* ---------- toasts ---------- */
  function toast(message) {
    var box = document.getElementById("toasts");
    if (!box) return;
    var el = document.createElement("div");
    el.className = "toast";
    el.textContent = message;
    box.appendChild(el);
    window.requestAnimationFrame(function () {
      el.classList.add("is-in");
    });
    window.setTimeout(function () {
      el.classList.remove("is-in");
      el.classList.add("is-out");
      window.setTimeout(function () {
        el.remove();
      }, 400);
    }, 3600);
  }

  document.addEventListener("DOMContentLoaded", function () {
    /* theme toggle */
    document.querySelectorAll("[data-theme-toggle]").forEach(function (button) {
      button.addEventListener("click", function () {
        var next = resolvedTheme() === "dark" ? "light" : "dark";
        stored = next;
        try {
          window.localStorage.setItem(STORAGE_KEY, next);
        } catch (err) {
          /* ignore */
        }
        applyTheme(next);
        track("theme-toggle", { theme: next });
      });
    });

    /* dialogs */
    document.querySelectorAll("[data-dialog-open]").forEach(function (trigger) {
      trigger.addEventListener("click", function (event) {
        var target = document.querySelector(trigger.getAttribute("data-dialog-open"));
        if (!target || typeof target.showModal !== "function") return;
        event.preventDefault();
        if (!target.open) target.showModal();
        var field = target.querySelector("input[name=word]");
        if (field) {
          window.setTimeout(function () {
            field.focus();
          }, 40);
        }
      });
    });
    document.querySelectorAll("dialog.dialog").forEach(function (dialog) {
      dialog.querySelectorAll("[data-dialog-close]").forEach(function (button) {
        button.addEventListener("click", function () {
          dialog.close();
        });
      });
      dialog.addEventListener("click", function (event) {
        if (event.target === dialog) dialog.close();
      });
    });

    /* server-rendered toast after a no-JS form post */
    document.querySelectorAll(".toast-server").forEach(function (el) {
      window.setTimeout(function () {
        el.remove();
      }, 5800);
    });

    /* add-entry flow */
    document.body.addEventListener("entryAdded", function (event) {
      var detail = event.detail || {};
      toast(detail.word ? "\u201C" + detail.word + "\u201D added to the ledger." : "Added to the ledger.");

      var dialog = document.getElementById("add-dialog");
      if (dialog && dialog.open) {
        dialog.close();
        var form = dialog.querySelector("form");
        if (form) form.reset();
      }
      var error = document.getElementById("form-error");
      if (error) error.textContent = "";

      if (detail.id) {
        var row = document.querySelector('[data-entry-id="' + detail.id + '"]');
        if (row) {
          row.classList.add("is-new");
          row.scrollIntoView({ block: "nearest", behavior: "smooth" });
        }
      }
      track("entry-added", { word: detail.word || "" });
    });

    /* search analytics */
    var searchTimer = null;
    var searchInput = document.querySelector('input[name="q"]');
    if (searchInput) {
      searchInput.addEventListener("input", function () {
        window.clearTimeout(searchTimer);
        var value = searchInput.value.trim();
        searchTimer = window.setTimeout(function () {
          if (value.length >= 2) track("search", { query: value });
        }, 900);
      });
    }

    /* ledger loading state */
    document.body.addEventListener("htmx:beforeRequest", function () {
      var ledger = document.getElementById("ledger");
      if (ledger) ledger.classList.add("is-loading");
    });
    ["htmx:afterSwap", "htmx:responseError", "htmx:sendError"].forEach(function (name) {
      document.body.addEventListener(name, function () {
        var ledger = document.getElementById("ledger");
        if (ledger) ledger.classList.remove("is-loading");
      });
    });
  });
})();
