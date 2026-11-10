(async function () {
  // Hangar bookmark importer (the fallback for browsers Hangar can't open itself). Runs on
  // robertsspaceindustries.com/<lang>/account/pledges while you are signed in, reads your pledge pages and the Hangar
  // log with hangarRead() (rsi_read.js) and sends them to Hangar on this computer; nothing goes anywhere else.
  var HANGAR = "__HANGAR_ORIGIN__";

  if (!/(^|\.)robertsspaceindustries\.com$/.test(location.hostname) || !/\/account\/pledges/.test(location.pathname)) {
    alert("Open your RSI pledges page first (My Hangar > Pledges), then click this bookmark again.");
    return;
  }

  var old = document.getElementById("hangar-import-box");
  if (old) old.remove();
  var box = document.createElement("div");
  box.id = "hangar-import-box";
  box.style.cssText = "position:fixed;top:16px;right:16px;z-index:2147483647;width:340px;padding:14px 16px;" +
    "background:#fff;color:#1d1d1f;border-radius:10px;box-shadow:0 8px 40px rgba(0,0,0,.45);" +
    "font:13px/1.45 -apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;text-align:left";
  document.body.appendChild(box);

  function show(title, lines, extra) {
    box.textContent = "";
    var h = document.createElement("div");
    h.textContent = title;
    h.style.cssText = "font-weight:700;font-size:14px;margin-bottom:4px";
    box.appendChild(h);
    (lines || []).forEach(function (l) {
      var d = document.createElement("div");
      d.textContent = l;
      box.appendChild(d);
    });
    if (extra) box.appendChild(extra);
    var x = document.createElement("a");
    x.textContent = "Close";
    x.href = "#";
    x.style.cssText = "display:inline-block;margin-top:8px;color:#0071e3;font-size:12px";
    x.onclick = function (e) { e.preventDefault(); box.remove(); };
    box.appendChild(x);
  }

  var data;
  try {
    data = await hangarRead(function (stage, current, total) {
      show(stage === "pledges" ? "Reading your pledges…" : "Reading your Hangar log…", ["Page " + current + " of " + total]);
    });
  } catch (e) {
    show("Couldn't read the pledges", [String(e.message || e)]);
    return;
  }

  // "complete" tells Hangar every page of the hangar and the log was read, so a pledge that isn't here has really left it.
  var payload = JSON.stringify({ source: "rsi", complete: !data.logNote, pledges: data.pledges, upgrades: data.upgrades, events: data.events });
  show("Sending to Hangar…", [data.pledges.length + " pledges, " + data.upgrades.length + " upgrade and " + data.events.length + " log entries"]);
  try {
    var r = await fetch(HANGAR + "/api/import/rsi", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Ships": "1" },
      body: payload
    });
    var result = await r.json();
    if (!r.ok) throw new Error(result.error || "Hangar returned " + r.status);
    var link = document.createElement("a");
    link.href = HANGAR + "/";
    link.target = "_blank";
    link.textContent = "Open Hangar";
    link.style.cssText = "display:block;margin-top:8px;color:#0071e3;font-weight:600";
    var up = result.upgrades || {};
    var lines = [
      result.created + " new, " + result.matched + " matched packages you already had, " + result.already_imported + " already imported.",
      up.applied ? up.applied + " upgraded ship" + (up.applied === 1 ? "" : "s") + " recorded" +
        (up.renamed ? " (" + up.renamed + " renamed to what you own now)." : ".") : "No new upgrades to record.",
      result.images_queued ? "Downloading " + result.images_queued + " pictures in the background." : "No new pictures needed."
    ];
    var fm = result.former || {};
    if (fm.melted || fm.gifted || fm.missing) {
      lines.splice(2, 0, "No longer in your hangar: " + [fm.melted && fm.melted + " melted", fm.gifted && fm.gifted + " gifted away",
        fm.missing && fm.missing + " unknown"].filter(Boolean).join(", ") + ".");
    }
    if (data.logNote) lines.push(data.logNote);
    show("Imported " + result.pledges + " pledges", lines, link);
  } catch (e) {
    // Hangar couldn't be reached from this page (browser or site security). Hand the data over by clipboard instead.
    var copied = false;
    try { await navigator.clipboard.writeText(payload); copied = true; } catch (_) { /* fall through to the text box */ }
    var area = null;
    if (!copied) {
      area = document.createElement("textarea");
      area.value = payload;
      area.readOnly = true;
      area.style.cssText = "width:100%;height:70px;margin-top:6px;font-size:11px";
      area.onclick = function () { area.select(); };
    }
    show(copied ? "Copied — now paste it into Hangar" : "Copy this into Hangar", [
      "This browser wouldn't let this page talk to Hangar directly (" + String(e.message || e) + ").",
      copied ? "In Hangar, click Import from RSI and paste." : "Select all, copy, then in Hangar click Import from RSI and paste."
    ], area);
  }
})();
