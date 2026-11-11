# Vim

Filetype detection, basic syntax and settings: copy `ftdetect/`, `ftplugin/`
and `syntax/` into `~/.vim` (or add this directory to `runtimepath`).

Language server with [vim-lsp](https://github.com/prabirshrestha/vim-lsp):

```vim
if executable('mio')
  autocmd User lsp_setup call lsp#register_server({
    \ 'name': 'mio',
    \ 'cmd': {server_info -> ['mio', 'lsp', '--stdio']},
    \ 'allowlist': ['mio'],
    \ })
endif
```

For a specific binary replace `'mio'` with its full path. Vim's regex syntax is
approximate; it follows the keyword list of the Mio lexer.
