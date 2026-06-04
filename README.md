# epub2zip

> [!NOTE]
> **AI agents / scripting**: read [llms-full.txt](llms-full.txt) for the
> invocation contract and unattended-use notes (pass `-y` to avoid the
> interactive overwrite prompt; exit codes are `0`/`1`/`2` — see below).

A robust Go command-line utility to extract sequential images from fixed-layout EPUBs (illustrated and art books) and archive them into standard ZIP files.

`epub2zip` is designed specifically for fixed-layout digital books, ensuring that the physical page layout (Left-to-Right or Right-to-Left) and structural alignment are preserved.

## Key Features

- **Fixed-Layout Intelligence**: Automatically detects whether an EPUB is fixed-layout or reflowable. Prevents accidental processing of text-only books.
- **Structural Book Parts**: Automatically identifies book sections (e.g., Cover, Main Body, TOC, Colophon) using EPUB 3 `landmarks`/`toc` or EPUB 2 `<guide>` metadata.
- **Human-Readable Naming**: Prefixes filenames with logical part names extracted from navigation links (e.g., `01_表紙_0001.jpg`).
- **Japanese Layout Support**: Robust handling of `rtl` (Right-to-Left) and `ltr` (Left-to-Right) reading directions.
- **Spread Alignment**: Automatically inserts alignment blanks (`_blank.png`) to ensure images land on the correct physical side in double-page readers, maintaining continuity across book parts.
- **Multi-Image Pages**: Handles logical pages that contain multiple image files, extracting them with `[PAGENO]_[INDEX]` naming to ensure no assets are lost.
- **Metadata Export**: Optionally extracts book metadata into a root `metadata.json` file.
- **Batch Processing**: Process multiple files at once with internal glob/wildcard support (works on Windows CMD/PowerShell).
- **Zero-Decompress Raw Copying**: Speeds up ZIP generation by up to 60x by copying raw compressed data blocks directly from the EPUB container, completely bypassing CPU-intensive decompression and re-compression.
- **Strict UTF-8 Encoding**: Enforces standard UTF-8 filename headers (`0x800` bit 11) for all ZIP entries, ensuring renamed files extract correctly across all non-Latin operating system locales without filename corruption (mojibake).

## Installation

### Pre-compiled Binaries
Pre-compiled binaries for Linux (x64), Windows (x64), and macOS (Apple Silicon) are available on the [GitHub Releases](https://github.com/mixcode/epub2zip/releases) page.

### Build from Source
```bash
go install github.com/mixcode/epub2zip@latest
```
*(Or clone the repository and run `go build`)*

## Usage

### Basic Conversion
Convert an EPUB to a ZIP in the current directory (prefixes parts by default):
```bash
epub2zip book.epub
```

### Combined Numbering
Include the global page number at the start of the filename:
```bash
epub2zip --total-numbering book.epub
# Output: 0004_02_目次_0001.jpg
```

### Custom Navigation Source
Select a specific EPUB 3 navigation block for part names:
```bash
epub2zip --nav-type landmarks book.epub
```

### Batch Processing
Convert all EPUBs in a folder and save them to a specific directory:
```bash
mkdir -p archive
epub2zip -o archive example_epub/*.epub
```

## CLI Flags

| Flag | Description | Default |
| :--- | :--- | :--- |
| `-o` | Output filename or directory | Current Dir |
| `-p` | Filename padding size (e.g., `-p 3` -> `001.jpg`) | `4` |
| `-v` | Enable verbose logging | `false` |
| `-d` | Dry run: list pages without creating the ZIP | `false` |
| `-b` | Blank page handling: `skip` or `generate` | `generate` |
| `--blank-color` | Color for blanks: `white`, `black`, `transparent`, or `#HEX` | `transparent` |
| `-m` | Metadata JSON mode: `none`, `compact`, `pretty` | `pretty` |
| `-c` | Compression mode: `raw` (copy compressed blocks), `deflate` (re-compress), or `store` (uncompressed) | `raw` |
| `-f` | Force execution on reflowable books | `false` |
| `-y` | Always overwrite existing files without prompting | `false` |
| `-q` | Quiet mode: suppress all STDOUT output; existing outputs are skipped (not prompted) unless `-y` | `false` |
| `--prefix-parts` | Prefix filenames with part names | `true` |
| `--total-numbering` | Include/use global page numbering | `false` |
| `--nav-type` | EPUB 3 navigation type: `toc` or `landmarks` | `toc` |

## Exit Codes

| Code | Meaning |
| :--- | :--- |
| `0` | All inputs converted successfully |
| `1` | Usage error (no input, invalid flag, or multiple inputs with a non-directory `-o`) |
| `2` | One or more inputs failed to process (details on stderr); other inputs in a batch still convert |

## Naming Schemes

The tool supports several naming conventions depending on your flags:

1.  **Default** (`--prefix-parts`): `[PartIdx]_[PartName]_[PartPageNum].ext` (e.g., `02_本編_0001.jpg`)
2.  **Combined** (`--prefix-parts --total-numbering`): `[GlobalNum]_[PartIdx]_[PartName]_[PartPageNum].ext` (e.g., `0012_02_本編_0010.jpg`)
3.  **Global Only** (`--total-numbering` only): `[GlobalNum].ext` (e.g., `0012.jpg`)
4.  **Simple** (both disabled): Same as Global Only (`[GlobalNum].ext`) to prevent filename collisions.

## Alignment Logic

`epub2zip` implements standard Japanese EPUB layout rules:
- **RTL Books**: Odd physical pages are on the **Left**, Even on the **Right**.
- **LTR Books**: Odd physical pages are on the **Right**, Even on the **Left**.

The tool tracks a **global physical index** to ensure that "left" and "right" spread images land on the correct side relative to the start of the book, automatically inserting alignment blanks (`_blank.png`) when necessary. This continuity is preserved even when transitioning between different book parts.

## License

MIT License. See [LICENSE](LICENSE) for details.
