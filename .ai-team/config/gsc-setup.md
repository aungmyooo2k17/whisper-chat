# Google Search Console MCP Setup

Instructions to enable GSC data access for the AI team.

## Prerequisites

1. Google Cloud Project
2. Google Search Console property (website verified)
3. Node.js 18+

## Step 1: Enable Search Console API

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Select or create a project
3. Navigate to **APIs & Services** > **Library**
4. Search for "Google Search Console API"
5. Click **Enable**

## Step 2: Create Service Account

1. Go to **APIs & Services** > **Credentials**
2. Click **Create Credentials** > **Service Account**
3. Name it (e.g., `gsc-mcp-access`)
4. Click **Create and Continue**
5. Skip role assignment (not needed for GSC)
6. Click **Done**

## Step 3: Generate Key

1. Click on the service account you created
2. Go to **Keys** tab
3. Click **Add Key** > **Create new key**
4. Select **JSON**
5. Download and save securely (e.g., `~/.config/gsc-credentials.json`)

## Step 4: Grant GSC Access

1. Go to [Google Search Console](https://search.google.com/search-console)
2. Select your property
3. Go to **Settings** > **Users and permissions**
4. Click **Add User**
5. Enter the service account email (from Step 2)
6. Set permission to **Full** (or **Restricted** for read-only)
7. Click **Add**

## Step 5: Install MCP Server

Install the GSC MCP server:

```bash
git clone https://github.com/ahonn/mcp-server-gsc.git
cd mcp-server-gsc
npm install
npm run build
```

## Step 6: Configure Claude Code

Add the GSC MCP to your Claude Code settings (`~/.claude.json` or `.claude/settings.json`):

```json
{
  "mcpServers": {
    "gsc": {
      "command": "node",
      "args": [
        "/path/to/mcp-server-gsc/dist/index.js"
      ],
      "env": {
        "GOOGLE_APPLICATION_CREDENTIALS": "/path/to/gsc-credentials.json",
        "GSC_PROPERTY": "sc-domain:example.com"
      }
    }
  }
}
```

**Replace**:
- `/path/to/mcp-server-gsc/` with the actual install path
- `/path/to/gsc-credentials.json` with your credentials file path
- `sc-domain:example.com` with your GSC property

## Step 7: Verify Setup

Restart Claude Code and test:
```
Ask: "What are the top search queries for my website?"
```

If working, you'll see GSC data in the response.

## Available GSC Tools

Once configured, the SEO Specialist can use:

| Tool | Purpose |
|------|---------|
| `gsc_search_analytics` | Get search performance data (queries, pages, clicks, impressions) |
| `gsc_list_sites` | List verified sites |
| `gsc_inspect_url` | Check indexing status of a URL |
| `gsc_list_sitemaps` | List submitted sitemaps |

## Troubleshooting

### "Permission denied"
- Verify service account email is added to GSC property
- Check credentials file path is correct

### "Property not found"
- Ensure `GSC_PROPERTY` matches exactly (including trailing slash if used in GSC)
- Try both `https://www.example.com/` and `sc-domain:example.com`

### "API not enabled"
- Go back to Google Cloud Console and enable Search Console API

## Security Notes

- Keep credentials JSON secure (never commit to git)
- Add to `.gitignore`: `**/gsc-credentials.json`
- Use restricted permissions if full access not needed
- Rotate keys periodically

## Resources

- [ahonn/mcp-server-gsc](https://github.com/ahonn/mcp-server-gsc)
- [Google Search Console API Docs](https://developers.google.com/webmaster-tools/v1/api_reference_index)
- [MCP Documentation](https://modelcontextprotocol.io/)
