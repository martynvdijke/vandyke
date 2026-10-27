/* vanDyke admin interactions: bulk selection, delete confirmation, photo preview. */
(function () {
  "use strict";

  function csrfToken() {
    var input = document.querySelector('input[name="csrf"]');
    return input ? input.value : "";
  }

  function ledgerBoxes() {
    return Array.prototype.slice.call(
      document.querySelectorAll('#bulk-form input[name="ids"]')
    );
  }

  function submitDelete(ids) {
    var form = document.createElement("form");
    form.method = "post";
    form.action = "/admin/entries/delete";
    form.style.display = "none";
    var csrf = document.createElement("input");
    csrf.type = "hidden";
    csrf.name = "csrf";
    csrf.value = csrfToken();
    form.appendChild(csrf);
    ids.forEach(function (id) {
      var input = document.createElement("input");
      input.type = "hidden";
      input.name = "ids";
      input.value = id;
      form.appendChild(input);
    });
    document.body.appendChild(form);
    form.submit();
  }

  document.addEventListener("change", function (event) {
    var target = event.target;
    if (!target) return;
    if (target.matches("[data-select-all]")) {
      ledgerBoxes().forEach(function (box) {
        box.checked = target.checked;
      });
      return;
    }
    if (target.matches('#bulk-form input[name="ids"]')) {
      var selectAll = document.querySelector("[data-select-all]");
      var boxes = ledgerBoxes();
      if (selectAll) {
        selectAll.checked = boxes.length > 0 && boxes.every(function (box) {
          return box.checked;
        });
      }
    }
  });

  document.addEventListener("click", function (event) {
    var bulk = event.target.closest("[data-bulk-delete]");
    if (bulk) {
      var ids = ledgerBoxes().filter(function (box) {
        return box.checked;
      }).map(function (box) {
        return box.value;
      });
      if (!ids.length) {
        event.preventDefault();
        return;
      }
      if (!window.confirm("Delete " + ids.length + " selected entr" + (ids.length === 1 ? "y" : "ies") + "?")) {
        event.preventDefault();
      }
      return;
    }

    var single = event.target.closest("[data-delete-id]");
    if (single) {
      var label = single.getAttribute("data-delete-label") || "this entry";
      if (!window.confirm("Delete " + label + "?")) return;
      submitDelete([single.getAttribute("data-delete-id")]);
    }
  });

  document.addEventListener("submit", function (event) {
    if (event.target.matches("[data-confirm-delete]")) {
      if (!window.confirm("Delete this entry and its photo?")) {
        event.preventDefault();
      }
    }
  });

  document.addEventListener("DOMContentLoaded", function () {
    var input = document.getElementById("edit-photo");
    var preview = document.getElementById("edit-photo-preview");
    var img = document.getElementById("edit-photo-preview-img");
    var objectURL = null;
    if (input && preview && img) {
      input.addEventListener("change", function () {
        if (objectURL) {
          URL.revokeObjectURL(objectURL);
          objectURL = null;
        }
        if (!input.files || !input.files[0]) {
          preview.hidden = true;
          img.removeAttribute("src");
          return;
        }
        objectURL = URL.createObjectURL(input.files[0]);
        img.src = objectURL;
        preview.hidden = false;
      });
    }

    document.body.addEventListener("htmx:beforeRequest", function () {
      var ledger = document.getElementById("admin-ledger");
      if (ledger) ledger.classList.add("is-loading");
    });
    ["htmx:afterSwap", "htmx:responseError", "htmx:sendError"].forEach(function (name) {
      document.body.addEventListener(name, function () {
        var ledger = document.getElementById("admin-ledger");
        if (ledger) ledger.classList.remove("is-loading");
      });
    });
  });
})();
