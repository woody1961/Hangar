// Reads the signed-in RSI account pages (pledges and the Hangar log). Shared by the bookmark and by Hangar's own RSI window,
// so both read exactly the same things. It only reads: it changes nothing on RSI and sends nothing anywhere itself.
// progress(stage, current, total) is called as pages are read; stage is "pledges" or "log".
// Returns {pledges, upgrades, events, logNote}. Throws if the pledges can't be read (e.g. not signed in).
async function hangarRead(progress) {
  var PAGE_DELAY_MS = 300;
  var pause = function () { return new Promise(function (r) { setTimeout(r, PAGE_DELAY_MS); }); };

  function text(el) { return el ? el.textContent.replace(/\s+/g, " ").trim() : ""; }

  function backgroundUrl(el) {
    var m = el && /url\(["']?([^"')]+)["']?\)/.exec(el.getAttribute("style") || "");
    return m ? new URL(m[1], location.href).href : null;
  }

  function parsePledges(doc) {
    return Array.prototype.map.call(doc.querySelectorAll("ul.list-items > li"), function (li) {
      var field = function (sel) { var i = li.querySelector(sel); return i ? i.value : ""; };
      var items = Array.prototype.map.call(li.querySelectorAll(".items .item"), function (it) {
        var liner = text(it.querySelector(".liner"));
        var m = /^(.*?)\s*\(\s*([^)]*?)\s*\)\s*$/.exec(liner);
        return {
          title: text(it.querySelector(".title")),
          kind: text(it.querySelector(".kind")),
          manufacturer: m ? m[1] : liner,
          code: m ? m[2] : "",
          image: backgroundUrl(it.querySelector(".image")),
          custom_name: text(it.querySelector(".custom-name-text"))
        };
      });
      // A gift nobody has claimed yet is still in your hangar, marked "Gifted" with a "Cancel Gift" button.
      var giftPending = Array.prototype.some.call(li.querySelectorAll("a, button"), function (el) { return /^cancel gift$/i.test(text(el)); }) ||
        /^gifted$/i.test(text(li.querySelector(".availability")));
      // Ships you can rename carry an ID of their own (membership_id); RSI gives no ID to the other ships and items.
      var nameable = [];
      try {
        var script = li.querySelector("script.js-pledge-nameable-ships");
        nameable = script ? JSON.parse(script.textContent) : [];
      } catch (e) { nameable = []; }
      return {
        nameable: Array.isArray(nameable) ? nameable : [],
        id: field(".js-pledge-id"),
        name: field(".js-pledge-name"),
        value: field(".js-pledge-value"),
        created: text(li.querySelector(".date-col")),
        gift_pending: giftPending,
        items: items
      };
    });
  }

  var lastPage = 1;
  Array.prototype.forEach.call(document.querySelectorAll(".pager a"), function (a) {
    var m = /[?&]page=(\d+)/.exec(a.getAttribute("href") || "");
    if (m) lastPage = Math.max(lastPage, +m[1]);
  });

  var pledges = [];
  for (var page = 1; page <= lastPage; page++) {
    progress("pledges", page, lastPage);
    var res = await fetch(location.pathname + "?page=" + page, { credentials: "same-origin" });
    if (!res.ok) throw new Error("RSI returned " + res.status + " for page " + page + " — are you still signed in?");
    var found = parsePledges(new DOMParser().parseFromString(await res.text(), "text/html"));
    if (!found.length) throw new Error("Page " + page + " had no pledges — are you still signed in?");
    pledges = pledges.concat(found);
    if (page < lastPage) await pause();
  }

  // The Hangar log records what happened to each pledge. Only the lines that matter are kept.
  var upgrades = [];
  var events = [];
  var logNote = "";
  try {
    var token = /(?:^|; )Rsi-Token=([^;]*)/.exec(document.cookie);
    var logPage = 1;
    var logPages = 1;
    do {
      progress("log", logPage, logPages);
      var lr = await fetch("/api/account/pledgeLog", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json", "Accept": "application/json", "x-rsi-token": token ? token[1] : "" },
        body: JSON.stringify({ page: logPage })
      });
      var lj = await lr.json();
      if (!lr.ok || !lj.success) throw new Error("RSI returned " + lr.status);
      logPages = lj.data.pagecount || 1;
      var ldoc = new DOMParser().parseFromString("<div>" + lj.data.rendered + "</div>", "text/html");
      Array.prototype.forEach.call(ldoc.querySelectorAll(".pledge-log-entry"), function (e) {
        var t = text(e);
        if (/Upgrade applied:/.test(t)) upgrades.push(t);
        // what happened to each pledge: created (its price), melted, gifted (and to whom), a gift cancelled, claimed, an upgrade used up, a ship reclaimed
        else if (/ - (Reclaimed by |Gifted to |Gift cancelled by |Configuration reclaimed by |Created by |Consumed by |Claimed as a gift by )/.test(t)) events.push(t);
      });
      logPage++;
      if (logPage <= logPages) await pause();
    } while (logPage <= logPages);
  } catch (e) {
    upgrades = [];
    events = [];
    logNote = "Couldn't read the Hangar log, so upgrades and melted/gifted pledges were skipped (" + String(e.message || e) + ").";
  }
  return { pledges: pledges, upgrades: upgrades, events: events, logNote: logNote };
}
