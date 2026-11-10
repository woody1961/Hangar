// Builds the Hangar desktop app into release/:
//   Hangar.app (and Hangar-macOS.zip)  one Mac app for Apple Silicon and Intel   (needs a Mac with Xcode's tools)
//   Hangar-Windows.exe                 Windows 10 and 11                         (builds anywhere)
//   Hangar-Linux-x64                   Linux                                     (built on Linux, or with Docker running)
//
//   npm run build
//
// Linux programs that draw windows need Linux's own graphics libraries to build, so they are made on a Linux computer
// (or inside Docker); scripts/build-linux.sh does that, and this script runs it when it can.
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, rmSync, writeFileSync, copyFileSync, chmodSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const version = JSON.parse(readFileSync(join(root, "package.json"), "utf8")).version;
const release = join(root, "release");
const scratch = mkdtempSync(join(tmpdir(), "hangar-build-"));
const built = [];
const skipped = [];

function go(args, env) {
  execFileSync("go", args, { cwd: root, stdio: "inherit", env: { ...process.env, ...env } });
}

function can(cmd, args = ["--version"]) {
  try { execFileSync(cmd, args, { stdio: "ignore" }); return true; } catch { return false; }
}

rmSync(release, { recursive: true, force: true });
mkdirSync(release, { recursive: true });

// ---------- the icon (drawn by scripts/icon) ----------
const iconPNG = join(scratch, "icon.png");
console.log("Drawing the icon");
go(["run", "./scripts/icon", iconPNG, join(root, "rsrc_windows_amd64.syso")], {}); // the .syso is built into the Windows program

// ---------- macOS: one app for both kinds of Mac ----------
if (process.platform === "darwin") {
  const arm = join(scratch, "hangar-arm64"), intel = join(scratch, "hangar-amd64");
  for (const [arch, out] of [["arm64", arm], ["amd64", intel]]) {
    console.log(`Building Hangar for macOS (${arch})`);
    go(["build", "-trimpath", "-ldflags", `-s -w -X main.version=${version}`, "-o", out, "."],
      { CGO_ENABLED: "1", GOOS: "darwin", GOARCH: arch, MACOSX_DEPLOYMENT_TARGET: "11.0", CGO_CFLAGS: "-mmacosx-version-min=11.0", CGO_LDFLAGS: "-mmacosx-version-min=11.0" });
  }
  const app = join(scratch, "Hangar.app"); // put together and signed outside synced folders, which tag files
  mkdirSync(join(app, "Contents", "MacOS"), { recursive: true });
  mkdirSync(join(app, "Contents", "Resources"), { recursive: true });
  execFileSync("lipo", ["-create", "-output", join(app, "Contents", "MacOS", "Hangar"), arm, intel]);
  const iconset = join(scratch, "Hangar.iconset");
  mkdirSync(iconset, { recursive: true });
  for (const px of [16, 32, 128, 256, 512]) {
    execFileSync("sips", ["-z", String(px), String(px), iconPNG, "--out", join(iconset, `icon_${px}x${px}.png`)], { stdio: "ignore" });
    execFileSync("sips", ["-z", String(px * 2), String(px * 2), iconPNG, "--out", join(iconset, `icon_${px}x${px}@2x.png`)], { stdio: "ignore" });
  }
  execFileSync("iconutil", ["-c", "icns", iconset, "-o", join(app, "Contents", "Resources", "Hangar.icns")]);
  writeFileSync(join(app, "Contents", "Info.plist"), `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Hangar</string>
  <key>CFBundleDisplayName</key><string>Hangar</string>
  <key>CFBundleIdentifier</key><string>app.hangar.desktop</string>
  <key>CFBundleExecutable</key><string>Hangar</string>
  <key>CFBundleIconFile</key><string>Hangar</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${version}</string>
  <key>CFBundleVersion</key><string>${version}</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>LSApplicationCategoryType</key><string>public.app-category.utilities</string>
</dict>
</plist>
`);
  // a local signature, so macOS lets it run on Apple Silicon (it is not notarised: see README.txt)
  try { execFileSync("xattr", ["-cr", app]); } catch { /* nothing to clear */ }
  execFileSync("codesign", ["--force", "--deep", "-s", "-", app]);
  execFileSync("ditto", ["-c", "-k", "--norsrc", "--noextattr", "--keepParent", app, join(release, "Hangar-macOS.zip")]);
  execFileSync("ditto", [app, join(release, "Hangar.app")]);
  // a copy right at the top of the app folder, so it's the first thing you see when you open the folder
  rmSync(join(root, "Hangar.app"), { recursive: true, force: true });
  execFileSync("ditto", [app, join(root, "Hangar.app")]);
  built.push("Hangar.app / Hangar-macOS.zip (macOS 11 or later, Apple Silicon and Intel)");
} else {
  skipped.push("macOS: build on a Mac");
}

// ---------- Windows: pure Go, builds anywhere ----------
console.log("Building Hangar for Windows");
go(["build", "-trimpath", "-ldflags", `-s -w -H windowsgui -X main.version=${version}`, "-o", join(release, "Hangar-Windows.exe"), "."],
  { CGO_ENABLED: "0", GOOS: "windows", GOARCH: "amd64" });
built.push("Hangar-Windows.exe (Windows 10 and 11)");

// ---------- Linux: on Linux itself, or inside Docker ----------
const linuxScript = join(root, "scripts", "build-linux.sh");
chmodSync(linuxScript, 0o755);
if (process.platform === "linux" || can("docker", ["info"])) {
  console.log("Building Hangar for Linux");
  execFileSync(linuxScript, [join(release, "Hangar-Linux-x64"), version], { cwd: root, stdio: "inherit" });
  built.push("Hangar-Linux-x64");
} else {
  skipped.push("Linux: needs a Linux computer or Docker (run scripts/build-linux.sh there)");
}

writeFileSync(join(release, "README.txt"), `Hangar ${version}
=========

A desktop app for your Star Citizen hangar. Nothing to install: pick the one for your computer.

How to start it
---------------
Mac: double-click Hangar.app. (To keep it, drag it into your Applications folder first, then start it from there or
from Launchpad. If macOS says it can't check the app, right-click it, choose Open, then Open again.)
Windows: double-click Hangar-Windows.exe.
The Hangar window opens straight away; no browser is needed. Close the window to quit.

${built.map((b) => "  " + b).join("\n")}
${skipped.length ? "\nNot built here:\n" + skipped.map((s) => "  " + s).join("\n") + "\n" : ""}
Mac: unzip Hangar-macOS.zip and drag Hangar to Applications. The first time, right-click it and choose Open
(macOS warns because the app isn't from the App Store or notarised).
Windows: double-click Hangar-Windows.exe. If SmartScreen warns about an unknown publisher, choose More info, then
Run anyway.
Linux: on the Linux computer, run scripts/build-linux.sh from the source folder (it needs Go and the packages it lists),
then run ./Hangar-Linux-x64.

Where your data lives
---------------------
Hangar keeps its database, pictures and backups in your own app-data folder:
  Mac      ~/Library/Application Support/Hangar
  Windows  %AppData%\\Hangar
  Linux    ~/.config/Hangar
Already have Hangar data from the earlier version? On the welcome screen choose "Use an existing Hangar data folder…"
and pick that folder (or the Hangar folder that holds it); or put the program in that Hangar folder, next to its
"data" folder and settings.json, and it uses them as they are. Settings → Data and backups shows the folder in use.

Importing from RSI: Import from RSI opens a Chrome, Edge or Brave window just for RSI; sign in there, then click
Continue. Without one of those browsers, use the bookmark method in the same dialog.

The full user guide is docs/USER-GUIDE.md in the source.

Disclaimer
----------
Hangar is an unofficial, fan-made tool. It is not affiliated with, endorsed or sponsored by Cloud Imperium Games or
Roberts Space Industries. Star Citizen®, Roberts Space Industries® and Cloud Imperium® are trademarks of Cloud Imperium
Rights LLC and Cloud Imperium Rights Ltd. Hangar only reads your own account pages, in your own signed-in browser, and
keeps everything on your computer. It is provided as is, with no warranty of any kind: keep backups, and use it at your
own risk.

Hangar is open source under the MIT License (LICENSE). The libraries it is built with are listed, with their licences,
in THIRD_PARTY_LICENSES.md.

Made entirely by AI ("vibe coded") with Claude Code and Craft Anything.
`);
copyFileSync(join(root, "THIRD_PARTY_LICENSES.md"), join(release, "THIRD_PARTY_LICENSES.md"));
copyFileSync(join(root, "LICENSE"), join(release, "LICENSE"));
// No website is made: Hangar is the desktop program above, with its own window. An old dist/ from earlier versions goes.
rmSync(join(root, "dist"), { recursive: true, force: true });

rmSync(scratch, { recursive: true, force: true });
console.log(`Built into ${release}:\n  ${built.join("\n  ")}`);
if (skipped.length) console.log(`Not built here:\n  ${skipped.join("\n  ")}`);
