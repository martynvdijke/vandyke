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
    document.querySelectorAll("[data-theme-toggle]").forEach(function (button) {
      button.setAttribute("aria-pressed", value === "dark" ? "true" : "false");
    });
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
    /* theme toggle: applyTheme may have run before the buttons were parsed */
    applyTheme(resolvedTheme());
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
    function setTriggersExpanded(dialog, expanded) {
      document.querySelectorAll("[data-dialog-open]").forEach(function (trigger) {
        if (trigger.getAttribute("data-dialog-open") === "#" + dialog.id) {
          trigger.setAttribute("aria-expanded", expanded ? "true" : "false");
        }
      });
    }
    document.querySelectorAll("[data-dialog-open]").forEach(function (trigger) {
      trigger.addEventListener("click", function (event) {
        var target = document.querySelector(trigger.getAttribute("data-dialog-open"));
        if (!target || typeof target.showModal !== "function") return;
        event.preventDefault();
        if (!target.open) target.showModal();
        setTriggersExpanded(target, true);
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
      dialog.addEventListener("close", function () {
        setTriggersExpanded(dialog, false);
      });
    });

    /* photo preview in the add dialog */
    var photoInput = document.querySelector('#add-dialog input[name="photo"]');
    var photoPreview = document.getElementById("photo-preview");
    var photoPreviewImg = document.getElementById("photo-preview-img");
    var previewURL = null;

    function clearPhotoPreview() {
      if (previewURL) {
        URL.revokeObjectURL(previewURL);
        previewURL = null;
      }
      if (photoPreviewImg) photoPreviewImg.removeAttribute("src");
      if (photoPreview) photoPreview.hidden = true;
    }

    function updatePhotoPreview() {
      clearPhotoPreview();
      if (!photoInput || !photoInput.files || !photoInput.files[0]) return;
      previewURL = URL.createObjectURL(photoInput.files[0]);
      if (photoPreviewImg) photoPreviewImg.src = previewURL;
      if (photoPreview) photoPreview.hidden = false;
    }

    if (photoInput) {
      photoInput.addEventListener("change", updatePhotoPreview);
      var addDialog = document.getElementById("add-dialog");
      if (addDialog) addDialog.addEventListener("close", clearPhotoPreview);
    }

    /* permalink share control: Web Share API, then the clipboard */
    function copyText(value, done, failed) {
      function fallback() {
        var input = document.createElement("input");
        input.value = value;
        input.setAttribute("readonly", "");
        input.style.position = "fixed";
        input.style.opacity = "0";
        document.body.appendChild(input);
        input.select();
        var ok = false;
        try {
          ok = document.execCommand("copy");
        } catch (err) {
          ok = false;
        }
        input.remove();
        if (ok) done();
        else failed();
      }
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(value).then(done, fallback);
      } else {
        fallback();
      }
    }

    document.querySelectorAll("[data-share-url]").forEach(function (link) {
      var label = link.textContent;
      var timer = null;
      link.addEventListener("click", function (event) {
        var url = link.getAttribute("data-share-url") || link.href || window.location.href;
        var title = link.getAttribute("data-share-title") || document.title;
        var text = link.getAttribute("data-share-text") || "";
        function copied() {
          toast("Link copied.");
          track("permalink-copy", { url: url });
          link.textContent = "Link copied";
          window.clearTimeout(timer);
          timer = window.setTimeout(function () {
            link.textContent = label;
          }, 2000);
        }
        function failed() {
          toast("Copy failed — select the address bar instead.");
        }
        if (typeof navigator.share === "function") {
          event.preventDefault();
          navigator.share({ title: title, text: text, url: url }).then(function () {
            track("permalink-share", { url: url });
          }, function (err) {
            if (err && err.name === "AbortError") return;
            copyText(url, copied, failed);
          });
          return;
        }
        event.preventDefault();
        copyText(url, copied, failed);
      });
    });

    /* slip of the day: hidden while a search term or filter is active */
    var slipSection = document.querySelector("[data-slip]");
    var toolbar = document.getElementById("search-form");
    if (slipSection && toolbar) {
      var updateSlip = function () {
        var active = false;
        ["q", "person", "year", "from", "to"].forEach(function (name) {
          var field = toolbar.querySelector('[name="' + name + '"]');
          if (field && String(field.value || "").trim() !== "") active = true;
        });
        var photo = toolbar.querySelector('[name="photo"]');
        if (photo && photo.checked) active = true;
        slipSection.hidden = active;
      };
      toolbar.addEventListener("input", updateSlip);
      toolbar.addEventListener("change", updateSlip);
      toolbar.addEventListener("search", updateSlip);
      updateSlip();
    }

    /* photo gallery lightbox: one native dialog, filled only when it opens */
    var lightbox = document.getElementById("photo-lightbox");
    var lightboxImg = lightbox ? lightbox.querySelector(".lightbox-img") : null;
    var lightboxCaption = lightbox ? lightbox.querySelector(".lightbox-caption") : null;
    var lightboxOpener = null;

    if (lightbox) {
      document.addEventListener("click", function (event) {
        var trigger = event.target && event.target.closest ? event.target.closest("[data-lightbox-src]") : null;
        if (!trigger || typeof lightbox.showModal !== "function") return;
        event.preventDefault();
        lightboxOpener = trigger;
        if (lightboxImg) {
          lightboxImg.src = trigger.getAttribute("data-lightbox-src") || "";
          lightboxImg.alt = trigger.getAttribute("data-lightbox-alt") || "";
        }
        if (lightboxCaption) lightboxCaption.textContent = trigger.getAttribute("data-lightbox-caption") || "";
        lightbox.showModal();
      });
      lightbox.addEventListener("close", function () {
        if (lightboxImg) lightboxImg.removeAttribute("src");
        if (lightboxOpener && typeof lightboxOpener.focus === "function") lightboxOpener.focus();
        lightboxOpener = null;
      });
    }

    /* server-rendered toast after a no-JS form post */
    document.querySelectorAll(".toast-server").forEach(function (el) {
      window.setTimeout(function () {
        el.remove();
      }, 5800);
    });

    /* add-entry flow */
    document.body.addEventListener("entryAdded", function (event) {
      var detail = event.detail || {};
      var added = detail.word ? "\u201C" + detail.word + "\u201D added to the ledger." : "Added to the ledger.";
      if (detail.photo && !detail.word) added = "Photo added to the ledger.";
      toast(added);

      var dialog = document.getElementById("add-dialog");
      if (dialog) {
        if (dialog.open) dialog.close();
        var form = dialog.querySelector("form");
        if (form) form.reset();
      }
      if (typeof clearPhotoPreview === "function") clearPhotoPreview();
      var error = document.getElementById("form-error");
      if (error) error.textContent = "";

      if (detail.id) {
        var row = document.querySelector('[data-entry-id="' + detail.id + '"]');
        if (row) {
          row.classList.add("is-new");
          row.scrollIntoView({ block: "nearest", behavior: "smooth" });
        }
      }
      track("entry-added", { word: detail.word || "", photo: !!detail.photo });
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
