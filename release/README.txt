Hangar 2.0.0
=========

A desktop app for your Star Citizen hangar. Nothing to install: pick the one for your computer.

How to start it
---------------
Mac: double-click Hangar.app. (To keep it, drag it into your Applications folder first, then start it from there or
from Launchpad. If macOS says it can't check the app, right-click it, choose Open, then Open again.)
Windows: double-click Hangar-Windows.exe.
The Hangar window opens straight away; no browser is needed. Close the window to quit.

  Hangar.app / Hangar-macOS.zip (macOS 11 or later, Apple Silicon and Intel)
  Hangar-Windows.exe (Windows 10 and 11)

Not built here:
  Linux: needs a Linux computer or Docker (run scripts/build-linux.sh there)

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
  Windows  %AppData%\Hangar
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
own risk. The libraries it is built with are listed, with their licences, in THIRD_PARTY_LICENSES.md.

Made entirely by AI ("vibe coded") with Claude Code and Craft Anything.
