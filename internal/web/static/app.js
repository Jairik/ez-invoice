(function () {
  "use strict";

  /* Toast auto-dismiss */
  var toast = document.querySelector("[data-toast]");
  if (toast) {
    setTimeout(function () {
      var reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      if (reduce) {
        toast.remove();
        return;
      }
      toast.style.transition = "opacity 0.35s ease, transform 0.35s ease";
      toast.style.opacity = "0";
      toast.style.transform = "translate(-50%, 12px)";
      setTimeout(function () { toast.remove(); }, 380);
    }, 3200);
  }

  /* Inline edit toggles: buttons with data-edit-toggle reveal the matching data-edit-form row */
  document.querySelectorAll("[data-edit-toggle]").forEach(function (button) {
    button.addEventListener("click", function () {
      var form = document.querySelector('[data-edit-form="' + button.dataset.editToggle + '"]');
      if (form) {
        form.hidden = !form.hidden;
        if (!form.hidden && form.querySelector("input,select")) {
          form.querySelector("input,select").focus();
        }
      }
    });
  });

  /* Destructive action confirmation */
  document.querySelectorAll("form[data-confirm]").forEach(function (form) {
    form.addEventListener("submit", function (event) {
      if (!window.confirm(form.dataset.confirm)) {
        event.preventDefault();
      }
    });
  });

  /* Preset selects: choosing Custom shows the free-form fields */
  function syncPresetFields() {
    document.querySelectorAll(".preset-select").forEach(function (select) {
      var target = document.querySelector('[data-custom="' + select.dataset.customTarget + '"]');
      if (target) {
        target.hidden = select.value !== "custom";
      }
    });
  }
  document.querySelectorAll(".preset-select").forEach(function (select) {
    select.addEventListener("change", syncPresetFields);
  });
  syncPresetFields();

  /* Settings: mark the local index entry for the section in view */
  var localnav = document.querySelector("[data-localnav]");
  if (localnav && "IntersectionObserver" in window) {
    var links = Array.prototype.slice.call(localnav.querySelectorAll("a"));
    var sections = links
      .map(function (link) { return document.querySelector(link.getAttribute("href")); })
      .filter(Boolean);

    var markActive = function (id) {
      links.forEach(function (link) {
        link.classList.toggle("is-active", link.getAttribute("href") === "#" + id);
      });
    };
    markActive(sections.length ? sections[0].id : "");

    var observer = new IntersectionObserver(function (records) {
      var visible = records
        .filter(function (record) { return record.isIntersecting; })
        .sort(function (a, b) { return a.boundingClientRect.top - b.boundingClientRect.top; });
      if (visible.length) {
        markActive(visible[0].target.id);
      }
    }, { rootMargin: "-10% 0px -70% 0px", threshold: 0 });
    sections.forEach(function (section) { observer.observe(section); });
  }

  /* Invoice builder */
  var form = document.getElementById("invoice-form");
  if (!form) {
    return;
  }

  var debounce;
  var rangeFrom = document.getElementById("range-from");
  var rangeTo = document.getElementById("range-to");
  var adjustment = document.getElementById("adjustment");

  function iso(date) {
    return date.getFullYear() + "-" +
      String(date.getMonth() + 1).padStart(2, "0") + "-" +
      String(date.getDate()).padStart(2, "0");
  }

  /* Period chips reload the page: the entry list for step 2 is server-rendered. */
  function reloadRange(from, to) {
    window.location.href = "/invoices/new?from=" + iso(from) + "&to=" + iso(to);
  }

  document.querySelectorAll("[data-range-presets] .chip").forEach(function (chip) {
    chip.addEventListener("click", function () {
      var now = new Date();
      var year = now.getFullYear();
      var month = now.getMonth();
      if (chip.dataset.range === "month") {
        reloadRange(new Date(year, month, 1), new Date(year, month + 1, 0));
      } else if (chip.dataset.range === "last") {
        reloadRange(new Date(year, month - 1, 1), new Date(year, month, 0));
      } else {
        var quarterStart = Math.floor(month / 3) * 3;
        reloadRange(new Date(year, quarterStart, 1), new Date(year, quarterStart + 3, 0));
      }
    });
  });

  /* Changing a date commits the same way, so step 2 always matches step 1. */
  function reloadFromInputs() {
    if (rangeFrom.value && rangeTo.value) {
      window.location.href = "/invoices/new?from=" + rangeFrom.value + "&to=" + rangeTo.value;
    }
  }
  rangeFrom.addEventListener("change", reloadFromInputs);
  rangeTo.addEventListener("change", reloadFromInputs);

  function selectedIDs() {
    return Array.prototype.map.call(
      form.querySelectorAll(".entry-check:checked"),
      function (input) { return Number(input.value); }
    );
  }

  function setText(id, value) {
    var node = document.getElementById(id);
    if (node) {
      node.textContent = value;
    }
  }

  function refreshPreview() {
    if (!rangeFrom.value || !rangeTo.value) {
      return;
    }
    var include = selectedIDs();
    fetch("/invoices/preview", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        from: rangeFrom.value,
        to: rangeTo.value,
        include: include,
        adjustment: adjustment.value
      })
    })
      .then(function (response) { return response.json(); })
      .then(function (data) {
        var error = document.getElementById("preview-error");
        if (data.error) {
          error.textContent = data.error;
          error.hidden = false;
          return;
        }
        error.hidden = true;

        setText("preview-count", String(data.count));
        setText("preview-entries", String(data.count));
        setText("preview-lock", String(data.count));
        setText("preview-hours", data.hours);
        setText("preview-subtotal", data.subtotal);
        setText("preview-adjustment", data.adjustment);
        setText("preview-total", data.total);

        setText("running-total", data.total);
        setText("running-count", String(data.count));
        setText("running-hours", data.hours);
        setText("running-subtotal", data.subtotal);
        setText("running-adjustment", data.adjustment);

        var chosen = document.getElementById("check-entries");
        if (chosen) {
          chosen.classList.toggle("is-off", data.count === 0);
        }
        var generate = document.getElementById("generate-btn");
        if (generate) {
          generate.disabled = data.count === 0;
        }
        var note = document.getElementById("running-note");
        if (note) {
          var excluded = form.querySelectorAll(".entry-check").length - data.count;
          note.textContent = excluded === 0
            ? "Every entry in this period is included."
            : excluded === 1
              ? "One entry is excluded. It stays open and will appear in the next period."
              : excluded + " entries are excluded. They stay open and will appear in the next period.";
        }

        form.querySelectorAll(".entry-row").forEach(function (row) {
          row.classList.toggle("is-unselected", include.indexOf(Number(row.dataset.entryId)) === -1);
        });
      })
      .catch(function (err) {
        var error = document.getElementById("preview-error");
        error.textContent = "Preview unavailable: " + err.message;
        error.hidden = false;
      });
  }

  function schedulePreview() {
    window.clearTimeout(debounce);
    debounce = window.setTimeout(refreshPreview, 160);
  }

  adjustment.addEventListener("input", schedulePreview);
  form.querySelectorAll(".entry-check").forEach(function (input) {
    input.addEventListener("change", schedulePreview);
  });

  /* Review block mirrors the chosen recipient. */
  var recipient = document.getElementById("recipient");
  if (recipient) {
    recipient.addEventListener("change", function () {
      var option = recipient.options[recipient.selectedIndex];
      setText("review-recipient", option ? option.textContent : "");
      setText("review-address", option ? option.dataset.address || "" : "");
    });
  }

  refreshPreview();
})();
