# ipfs-check Web Interface

This directory contains the static web assets for ipfs-check.

## Deployment

`./web` is automatically deployed to https://check.ipfs.network via GitHub Pages.

Backend updates require manual deployment via `ipshipyard/waterworks-infra` ([example](https://github.com/ipshipyard/waterworks-infra/pull/740)).

## Architecture

The files in this directory are embedded directly into the Go binary and served as-is. No build step is required for normal deployment since the compiled CSS (`output.css`) is already committed to the repository.

## Development

### Prerequisites

- Node.js and npm (only needed for modifying styles)

### Modifying Styles

The project uses Tailwind CSS for styling. If you need to make style changes:

1. Install dependencies (one-time setup):
   ```bash
   npm ci
   ```

2. For development with live CSS updates:
   ```bash
   npm run dev
   ```
   This watches `input.css` for changes and automatically rebuilds `output.css`

3. For production build (minified CSS):
   ```bash
   npm run build
   ```

4. **Important**: After making style changes, commit the updated `output.css` file

### Files

- `index.html` - Main HTML file
- `script.js` - JavaScript logic for the check interface
- `input.css` - Source Tailwind CSS file (modify this for style changes)
- `output.css` - Compiled CSS (auto-generated, but committed to git)
- `favicon.ico` - Site favicon
- `fonts/` - Custom fonts

### Notes

- The `output.css` file is intentionally committed to version control
- This allows the Go binary to embed and serve the web interface without requiring a build step
- Only run `npm ci` and `npm run build` if you're modifying the styles