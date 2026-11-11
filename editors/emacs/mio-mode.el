;;; mio-mode.el --- Major mode and Eglot setup for Mio  -*- lexical-binding: t; -*-
;; Requires Emacs 29+ (Eglot built in).  (load "/path/to/mio-mode.el")
(defgroup mio nil "Mio language support." :group 'languages)
(defcustom mio-executable "mio" "Path to the mio executable." :type 'string :group 'mio)

(defconst mio-font-lock-keywords
  `((,(regexp-opt '("kaku" "waza" "teisu" "kata" "sentaku" "koukai"
                    "moshi" "sore" "aida" "awase" "baai" "kaesu" "nukeru" "tsuzuku" "yobu") 'symbols)
     . font-lock-keyword-face)
    (,(regexp-opt '("hai" "iie" "mu") 'symbols) . font-lock-constant-face)
    (,(regexp-opt '("int8" "int16" "int32" "int64" "uint8" "uint16" "uint32" "float32"
                    "float64" "rune" "bool" "string" "unit" "resource" "Map") 'symbols)
     . font-lock-type-face)))

(defvar mio-mode-syntax-table
  (let ((st (make-syntax-table)))
    (modify-syntax-entry ?/ ". 124b" st)
    (modify-syntax-entry ?* ". 23" st)
    (modify-syntax-entry ?\n "> b" st)
    (modify-syntax-entry ?_ "_" st)
    st))

;;;###autoload
(define-derived-mode mio-mode prog-mode "Mio"
  (setq-local font-lock-defaults '(mio-font-lock-keywords))
  (setq-local comment-start "// ")
  (setq-local indent-tabs-mode nil)
  (setq-local tab-width 4))

;;;###autoload
(add-to-list 'auto-mode-alist '("\\.mio\\'" . mio-mode))

(with-eval-after-load 'eglot
  (add-to-list 'eglot-server-programs
               `(mio-mode . (,mio-executable "lsp" "--stdio"))))
(provide 'mio-mode)
;;; mio-mode.el ends here
