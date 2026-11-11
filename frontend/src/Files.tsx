import { DragEvent, useCallback, useEffect, useRef, useState } from 'react'
import { ChevronRight, Download, File as FileIcon, FilePlus, Folder, FolderPlus, Link2, Pencil, Trash2, Upload } from 'lucide-react'
import { ApiError, api } from './api'
import { errMsg, useT } from './i18n'
import { toast } from './store'
import { Field, Modal, fmtBytes } from './ui'

type Entry = { name: string; dir: boolean; size: number; mtime: number }
const join = (a: string, b: string) => (a.endsWith('/') ? a : a + '/') + b
const q = (p: string) => encodeURIComponent(p)
const textLike = /\.(txt|json|ya?ml|properties|toml|cfg|conf|ini|log|md|js|sh|xml|mcmeta|csv|secret|lang|json5|txt)$/i

export function FilesTab({ id }: { id: string }) {
  const t = useT()
  const [path, setPath] = useState('/')
  const [items, setItems] = useState<Entry[] | null>(null)
  const [sel, setSel] = useState<Set<string>>(new Set())
  const [modal, setModal] = useState<null | 'mkdir' | 'newfile' | 'url' | { rename: string } | { edit: string }>(null)
  const [drag, setDrag] = useState(false)
  const [busy, setBusy] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const base = `/instances/${id}/files`

  const load = useCallback(() => {
    api<Entry[]>(`${base}?path=${q(path)}`).then((l) => { setItems(l); setSel(new Set()) }).catch((e) => { toast(errMsg(e.code), 'err'); if (path !== '/') setPath('/') })
  }, [base, path])
  useEffect(() => { setItems(null); load() }, [load])

  const crumbs = path.split('/').filter(Boolean)
  const err = (e: unknown) => toast(errMsg((e as ApiError).code), 'err')

  const upload = async (files: FileList | File[]) => {
    const list = Array.from(files); if (!list.length) return
    setBusy(true)
    try {
      const fd = new FormData(); list.forEach((f) => fd.append('files', f, f.name))
      await api(`${base}/upload?path=${q(path)}`, { raw: fd }); load()
    } catch (e) { err(e) } finally { setBusy(false) }
  }
  const onDrop = (e: DragEvent) => { e.preventDefault(); setDrag(false); if (e.dataTransfer.files.length) upload(e.dataTransfer.files) }
  const remove = async () => {
    if (!sel.size || !confirm(t('files.deleteConfirm'))) return
    try { await api(`${base}/delete`, { body: { paths: [...sel].map((n) => join(path, n)) } }); load() } catch (e) { err(e) }
  }
  const toggle = (n: string) => setSel((s) => { const x = new Set(s); x.has(n) ? x.delete(n) : x.add(n); return x })
  const open = (e: Entry) => (e.dir ? setPath(join(path, e.name)) : e.size <= 2 << 20 ? setModal({ edit: join(path, e.name) }) : toast(t('files.tooLarge'), 'err'))
  const only = sel.size === 1 ? [...sel][0] : null

  return (
    <div className="space-y-3" onDragOver={(e) => { e.preventDefault(); setDrag(true) }} onDragLeave={() => setDrag(false)} onDrop={onDrop}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <nav className="flex items-center flex-wrap text-sm" aria-label="Path">
          <button className="px-1.5 py-1 rounded-md hover:bg-raised" onClick={() => setPath('/')}>/</button>
          {crumbs.map((c, i) => (
            <span key={i} className="flex items-center"><ChevronRight size={14} className="text-ink-mute" />
              <button className="px-1.5 py-1 rounded-md hover:bg-raised" onClick={() => setPath('/' + crumbs.slice(0, i + 1).join('/'))}>{c}</button></span>
          ))}
        </nav>
        <div className="flex flex-wrap gap-2">
          <input ref={fileInput} type="file" multiple hidden onChange={(e) => e.target.files && upload(e.target.files)} />
          <button className="btn-primary btn-sm" disabled={busy} onClick={() => fileInput.current?.click()}><Upload size={14} />{t('files.upload')}</button>
          <button className="btn-neutral btn-sm" onClick={() => setModal('url')}><Link2 size={14} />{t('files.url')}</button>
          <button className="btn-neutral btn-sm" onClick={() => setModal('mkdir')}><FolderPlus size={14} />{t('files.folder')}</button>
          <button className="btn-neutral btn-sm" onClick={() => setModal('newfile')}><FilePlus size={14} />{t('files.file')}</button>
        </div>
      </div>

      {sel.size > 0 && (
        <div className="card flex flex-wrap items-center gap-2 px-3 py-2 text-sm">
          <span className="text-ink-mute mr-auto">{sel.size} ✓</span>
          {only && <button className="btn-neutral btn-sm" onClick={() => setModal({ rename: only })}><Pencil size={13} />{t('files.rename')}</button>}
          {only && <a className="btn-neutral btn-sm" href={`/api${base}/download?path=${q(join(path, only))}`}><Download size={13} />{t('files.download')}</a>}
          <button className="btn-danger btn-sm" onClick={remove}><Trash2 size={13} />{t('files.delete')}</button>
        </div>
      )}

      <div className={`card overflow-hidden relative ${drag ? 'border-accent' : ''}`}>
        {drag && <div className="absolute inset-0 z-10 grid place-items-center bg-bg/85 text-sm">{t('files.drop')}</div>}
        {items === null ? <p className="p-6 text-sm text-ink-mute">{t('common.loading')}</p> : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead><tr className="text-left text-ink-mute border-b border-line">
                <th className="w-10 p-3" /><th className="p-3 font-normal">{t('files.name')}</th>
                <th className="p-3 font-normal hidden sm:table-cell w-28">{t('files.size')}</th><th className="p-3 font-normal hidden md:table-cell w-44">{t('files.modified')}</th>
              </tr></thead>
              <tbody className="divide-y divide-line">
                {path !== '/' && <tr className="hover:bg-raised/60 cursor-pointer" onClick={() => setPath('/' + crumbs.slice(0, -1).join('/'))}><td /><td className="p-3 text-ink-mute" colSpan={3}>..</td></tr>}
                {items.length === 0 && <tr><td colSpan={4} className="p-8 text-center text-ink-mute">{t('files.empty')}</td></tr>}
                {items.map((e) => (
                  <tr key={e.name} className={`hover:bg-raised/60 ${sel.has(e.name) ? 'bg-accent/10' : ''}`}>
                    <td className="p-3"><input type="checkbox" className="h-4 w-4 accent-[#8C2B32]" checked={sel.has(e.name)} onChange={() => toggle(e.name)} aria-label={e.name} /></td>
                    <td className="p-3"><button className="flex items-center gap-2.5 text-left w-full" onClick={() => open(e)}>
                      {e.dir ? <Folder size={16} className="text-olive-light shrink-0" /> : <FileIcon size={16} className="text-ink-mute shrink-0" />}<span className="break-all">{e.name}</span></button></td>
                    <td className="p-3 hidden sm:table-cell text-ink-mute tabular-nums">{e.dir ? '' : fmtBytes(e.size)}</td>
                    <td className="p-3 hidden md:table-cell text-ink-mute">{new Date(e.mtime * 1000).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {modal === 'mkdir' && <NameModal title={t('files.folder')} onClose={() => setModal(null)} onOk={async (n) => { try { await api(`${base}/mkdir`, { body: { path: join(path, n) } }); setModal(null); load() } catch (e) { err(e) } }} />}
      {modal === 'newfile' && <NameModal title={t('files.file')} onClose={() => setModal(null)} onOk={async (n) => { try { await api(`${base}/content?path=${q(join(path, n))}`, { method: 'PUT', raw: '' }); setModal(null); load(); setModal({ edit: join(path, n) }) } catch (e) { err(e) } }} />}
      {modal === 'url' && <UrlModal onClose={() => setModal(null)} onOk={async (url) => { try { await api(`${base}/fetch`, { body: { url, path } }); setModal(null); load() } catch (e) { err(e) } }} />}
      {modal && typeof modal === 'object' && 'rename' in modal && <NameModal title={t('files.rename')} initial={modal.rename} onClose={() => setModal(null)}
        onOk={async (n) => { try { await api(`${base}/rename`, { body: { from: join(path, modal.rename), to: join(path, n) } }); setModal(null); load() } catch (e) { err(e) } }} />}
      {modal && typeof modal === 'object' && 'edit' in modal && <Editor base={base} file={modal.edit} onClose={() => setModal(null)} onSaved={load} />}
    </div>
  )
}

function NameModal({ title, initial = '', onClose, onOk }: { title: string; initial?: string; onClose: () => void; onOk: (n: string) => void }) {
  const t = useT(); const [n, setN] = useState(initial)
  return (
    <Modal title={title} onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); n.trim() && onOk(n.trim()) }} className="space-y-4">
        <Field label={t('files.newName')}><input className="input" value={n} onChange={(e) => setN(e.target.value)} autoFocus pattern="[^/\\]+" /></Field>
        <div className="flex justify-end gap-2"><button type="button" className="btn-neutral" onClick={onClose}>{t('common.cancel')}</button><button className="btn-primary">{t('common.confirm')}</button></div>
      </form>
    </Modal>
  )
}

function UrlModal({ onClose, onOk }: { onClose: () => void; onOk: (u: string) => Promise<void> }) {
  const t = useT(); const [u, setU] = useState(''); const [busy, setBusy] = useState(false)
  return (
    <Modal title={t('files.url')} onClose={onClose}>
      <form onSubmit={async (e) => { e.preventDefault(); setBusy(true); await onOk(u.trim()); setBusy(false) }} className="space-y-4">
        <Field label="URL" hint={t('files.urlHint')}><input className="input font-mono" type="url" placeholder="https://" value={u} onChange={(e) => setU(e.target.value)} autoFocus required /></Field>
        <div className="flex justify-end gap-2"><button type="button" className="btn-neutral" onClick={onClose}>{t('common.cancel')}</button><button className="btn-primary" disabled={busy}>{t('files.download')}</button></div>
      </form>
    </Modal>
  )
}

function Editor({ base, file, onClose, onSaved }: { base: string; file: string; onClose: () => void; onSaved: () => void }) {
  const t = useT(); const [text, setText] = useState<string | null>(null); const [orig, setOrig] = useState('')
  useEffect(() => { api<string>(`${base}/content?path=${q(file)}`).then((s) => { const v = typeof s === 'string' ? s : JSON.stringify(s, null, 2); setText(v); setOrig(v) }).catch((e) => { toast(errMsg(e.code), 'err'); onClose() }) }, [])
  const save = async () => { try { await api(`${base}/content?path=${q(file)}`, { method: 'PUT', raw: text ?? '' }); setOrig(text ?? ''); toast(t('files.saved')); onSaved() } catch (e) { toast(errMsg((e as ApiError).code), 'err') } }
  const dirty = text !== orig
  return (
    <Modal title={file} onClose={() => (!dirty || confirm(t('common.confirm') + '?')) && onClose()} wide>
      {text === null ? <p className="text-ink-mute">{t('common.loading')}</p> : (
        <div className="space-y-3">
          {!textLike.test(file) && /[\x00-\x08]/.test(text.slice(0, 2000)) && <p className="text-sm text-warn">Binary file?</p>}
          <textarea className="w-full h-[55vh] rounded-xl bg-bg border border-line p-3 font-mono text-[13px] leading-relaxed focus:border-accent focus:outline-none resize-none" value={text}
            onChange={(e) => setText(e.target.value)} spellCheck={false} onKeyDown={(e) => { if ((e.ctrlKey || e.metaKey) && e.key === 's') { e.preventDefault(); save() } }} />
          <div className="flex justify-end gap-2"><button className="btn-neutral" onClick={onClose}>{t('common.close')}</button><button className="btn-primary" disabled={!dirty} onClick={save}>{t('files.save')}</button></div>
        </div>
      )}
    </Modal>
  )
}
