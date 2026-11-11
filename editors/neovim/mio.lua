-- Mio for Neovim (0.11+). Source from init.lua:  dofile(vim.fn.expand("~/path/to/editors/neovim/mio.lua"))
-- or copy it to ~/.config/nvim/plugin/mio.lua.
-- Set vim.g.mio_executable before loading to use a specific binary.
local exe = vim.g.mio_executable or "mio"

vim.filetype.add({ extension = { mio = "mio" } })

vim.api.nvim_create_autocmd("FileType", {
  pattern = "mio",
  callback = function(args)
    vim.bo[args.buf].commentstring = "// %s"
    vim.bo[args.buf].expandtab = true
    vim.bo[args.buf].shiftwidth = 4
  end,
})

vim.lsp.config("mio", {
  cmd = { exe, "lsp", "--stdio" },
  filetypes = { "mio" },
  -- Projects are located by the nearest bunko/ directory or git root.
  root_markers = { "bunko", ".git" },
})
vim.lsp.enable("mio")

-- Tree-sitter: build the parser from tools/tree-sitter-mio (see README) and register it.
-- vim.treesitter.language.register("mio", "mio")
