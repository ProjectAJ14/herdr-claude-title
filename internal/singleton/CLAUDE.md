# internal/singleton — one instance per session

Herdr runs a plugin's startup hook at every server start and live handoff, and
keeps no handle on what it started. Two instances disagreeing (say, after a
settings change) each read the other's renames as the user's and lock every tab.

- **The claim**: `instances/<sha256(socket path)[:8]>.json` in the state dir
  holds the pid. The socket names the session, so two sessions never displace
  each other. Newest wins: every poll checks the claim, and a newer pid means leave.
- `Take` waits up to `LeaveTimeout` for the displaced pid to exit, **killing
  nothing**; an instance too old to check claims is warned about.
- **Nothing is removed on exit.** A claim naming a dead pid displaces nobody,
  and deleting it would race a successor that just wrote its own.
- **Ready** is a separate file, so marking ready can never overwrite a newer claim.
- **`restart`** starts the binary detached with stdio on the null device: Herdr
  reads an action's output to EOF, so a child holding its pipes keeps the action
  running and holds one of 32 plugin command slots. It then waits for the new pid
  to hold the claim, be ready, and for the old pid to be gone.
- The claim uses the platform state dir (`os.UserConfigDir()`), never the first
  config dir that has a file, so every instance agrees on where it is.
