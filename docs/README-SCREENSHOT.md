# README screenshot capture

Use this flow after changing a surface shown in `README.md`. It keeps fixture data and Herdr state out of the repository, while making the final capture a maintainer decision.

## 1. Prepare an isolated fixture

Start a separate Herdr session with a disposable `HOME` / XDG tree. Populate the agent panes, usage data, cache bands, and sidebar configuration needed to demonstrate the changed behavior. Do not use a personal Herdr session or its credentials.

Confirm the prepared session before opening Ghostty:

```sh
HERDR_ENV=1 herdr --session <session> pane layout
```

## 2. Open the capture surface

With Ghostty already running, open the isolated session in a dedicated window:

```sh
scripts/open-readme-screenshot.sh \
  --session <session> \
  --home /path/to/disposable-home \
  --herdr /path/to/herdr
```

The launcher verifies that the requested Herdr session is reachable, opens a new Ghostty window, and attaches it to that session. It does not start a server, prepare fixture data, capture a screenshot, or alter `docs/assets/agent-usage-pane.png`.

## 3. Capture and replace the asset

The maintainer takes and reviews the screenshot manually. Replace `docs/assets/agent-usage-pane.png` with the approved image, then inspect the rendered `README.md` image before committing.

Leave the isolated session running until the maintainer has accepted the screenshot. Close only its dedicated Ghostty window and stop only its dedicated server afterward.
