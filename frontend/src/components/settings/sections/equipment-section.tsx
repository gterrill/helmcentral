import { useEffect, useMemo, useRef, useState, type ChangeEvent } from 'react'
import { Upload, Download, PencilLine, Trash2, Plus } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { apiBaseUrl } from '@/config/api'
import { useEquipmentProfiles } from '@/hooks/use-equipment-profiles'
import type { EngineProfile, EquipmentProfileValidationError } from '@/lib/engine-profiles'

type ProfileKind = 'engine' | 'generator'

const defaultProfile = {
  schema_version: 1,
  kind: 'engine' as ProfileKind,
  id: 'cummins-qsb67-550',
  name: 'Cummins QSB 6.7 550',
  file: 'cummins-qsb67-550.json',
  json: `{
  "id": "cummins-qsb67-550",
  "name": "Cummins QSB 6.7 550",
  "gauge_count": 8
}`,
}

const newProfileTemplates: Record<ProfileKind, EngineProfile> = {
  engine: {
    schema_version: 1,
    kind: 'engine',
    id: 'new-engine-profile',
    name: 'New engine profile',
    gauges: [
      {
        path_suffix: 'oilPressure',
        label: 'Oil Pressure',
        display: 'radial',
        quantity: 'pressure',
        unit: 'psi',
        min: 0,
        max: 100,
      },
    ],
  },
  generator: {
    schema_version: 1,
    kind: 'generator',
    id: 'new-generator-profile',
    name: 'New generator profile',
    gauges: [
      {
        path_suffix: 'phase.A.frequency',
        label: 'Frequency',
        display: 'numeric',
        quantity: 'frequency',
        unit: 'Hz',
      },
    ],
  },
}

function createEditableDraft(kind: ProfileKind) {
  const template = newProfileTemplates[kind]
  return {
    schema_version: 1,
    kind,
    id: template.id,
    name: template.name,
    file: `${template.id}.json`,
    json: JSON.stringify(template, null, 2),
  }
}

function toEditableProfile(profile: EngineProfile) {
  return {
    schema_version: profile.schema_version ?? 1,
    kind: profile.kind ?? 'engine',
    id: profile.id,
    name: profile.name,
    file: `${profile.id}.json`,
    json: JSON.stringify(profile, null, 2),
  }
}

export function EquipmentSection() {
  const { profiles, loading, reload } = useEquipmentProfiles(true)
  const uploadInputRef = useRef<HTMLInputElement | null>(null)
  const [isEditing, setIsEditing] = useState(false)
  const [isCreating, setIsCreating] = useState(false)
  const [profile, setProfile] = useState(defaultProfile)
  const [selectedID, setSelectedID] = useState('')
  const [saving, setSaving] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [jsonError, setJsonError] = useState<string | null>(null)
  const [validationErrors, setValidationErrors] = useState<EquipmentProfileValidationError[]>([])

  const selectedProfile = useMemo(
    () => profiles.find((item) => item.id === selectedID) ?? profiles[0],
    [profiles, selectedID],
  )

  useEffect(() => {
    if (!selectedProfile) {
      setSelectedID('')
      setProfile(defaultProfile)
      setIsCreating(false)
      setIsEditing(false)
      return
    }

    setSelectedID(selectedProfile.id)
    setProfile(toEditableProfile(selectedProfile))
    setIsCreating(false)
    setIsEditing(false)
    setError(null)
    setJsonError(null)
    setValidationErrors([])
  }, [selectedProfile?.id])

  const handleEdit = () => setIsEditing(true)

  const handleNew = () => {
    setIsCreating(true)
    setIsEditing(true)
    setSelectedID('')
    setError(null)
    setJsonError(null)
    setValidationErrors([])
    setProfile(createEditableDraft('engine'))
  }

  const handleNewKindChange = (kind: ProfileKind) => {
    setError(null)
    setJsonError(null)
    setValidationErrors([])
    setProfile(createEditableDraft(kind))
  }

  const handleDownload = () => {
    if (!profile.id) return
    window.open(`${apiBaseUrl}/api/equipment-profiles/${encodeURIComponent(profile.id)}/download`, '_blank')
  }

  const handleUploadClick = () => {
    uploadInputRef.current?.click()
  }

  const handleUploadChange = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file) return

    setError(null)
    setJsonError(null)
    setValidationErrors([])

    let parsed: EngineProfile
    try {
      const text = await file.text()
      parsed = JSON.parse(text) as EngineProfile
    } catch {
      setJsonError('Uploaded file is not valid JSON.')
      event.target.value = ''
      return
    }

    if (typeof parsed?.id !== 'string' || parsed.id.trim() === '') {
      setJsonError('Uploaded profile must include a non-empty string id.')
      event.target.value = ''
      return
    }

    try {
      setUploading(true)
      const response = await fetch(`${apiBaseUrl}/api/equipment-profiles`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(parsed),
      })

      const payload = (await response.json().catch(() => null)) as {
        error?: string
        errors?: EquipmentProfileValidationError[]
        profile?: EngineProfile
      } | null

      if (!response.ok) {
        if (Array.isArray(payload?.errors) && payload.errors.length > 0) {
          setValidationErrors(payload.errors)
        }
        throw new Error(payload?.error ?? 'Unable to upload profile.')
      }

      const uploaded = payload?.profile ?? parsed
      setSelectedID(uploaded.id)
      setProfile(toEditableProfile(uploaded))
      setIsCreating(false)
      setIsEditing(false)
      reload()
    } catch (uploadError) {
      setError(uploadError instanceof Error ? uploadError.message : 'Unable to upload profile.')
    } finally {
      setUploading(false)
      event.target.value = ''
    }
  }

  const handleDelete = async () => {
    if (!profile.id || isCreating) return
    setError(null)
    setValidationErrors([])
    try {
      setDeleting(true)
      const response = await fetch(`${apiBaseUrl}/api/equipment-profiles/${encodeURIComponent(profile.id)}`, {
        method: 'DELETE',
      })
      if (!response.ok) {
        const payload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(payload?.error ?? 'Unable to delete profile.')
      }

      setSelectedID('')
      setProfile(defaultProfile)
      setIsEditing(false)
      setIsCreating(false)
      reload()
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : 'Unable to delete profile.')
    } finally {
      setDeleting(false)
    }
  }

  const handleSave = async () => {
    setError(null)
    setJsonError(null)
    setValidationErrors([])

    let parsed: EngineProfile
    try {
      parsed = JSON.parse(profile.json) as EngineProfile
    } catch {
      setJsonError('Profile JSON is invalid. Fix parsing errors before saving.')
      return
    }

    if (!parsed || typeof parsed !== 'object') {
      setJsonError('Profile JSON must be a JSON object.')
      return
    }

    if (typeof parsed.id !== 'string' || parsed.id.trim() === '') {
      setJsonError('Profile JSON must include a non-empty string id.')
      return
    }

    if (parsed.id !== profile.id) {
      setJsonError('Profile JSON id must match the selected profile id.')
      return
    }

    if (typeof parsed.name !== 'string' || parsed.name.trim() === '') {
      setJsonError('Profile JSON must include a non-empty string name.')
      return
    }

    try {
      setSaving(true)
      const payload = {
        ...parsed,
        schema_version: profile.schema_version,
        kind: profile.kind,
        name: profile.name,
      }
      const endpoint = isCreating
        ? `${apiBaseUrl}/api/equipment-profiles`
        : `${apiBaseUrl}/api/equipment-profiles/${encodeURIComponent(profile.id)}`

      const response = await fetch(endpoint, {
        method: isCreating ? 'POST' : 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })

      if (!response.ok) {
        const payload = (await response.json().catch(() => null)) as {
          error?: string
          errors?: EquipmentProfileValidationError[]
        } | null
        if (Array.isArray(payload?.errors) && payload.errors.length > 0) {
          setValidationErrors(payload.errors)
        }
        throw new Error(payload?.error ?? 'Unable to save profile.')
      }

      setProfile((current) => ({ ...current, json: JSON.stringify(payload, null, 2) }))
      if (isCreating) {
        setSelectedID(payload.id)
        setIsCreating(false)
      }
      setIsEditing(false)
      reload()
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : 'Unable to save profile.')
    } finally {
      setSaving(false)
    }
  }

  const handleCancel = () => {
    if (selectedProfile) {
      setProfile(toEditableProfile(selectedProfile))
    }
    setError(null)
    setJsonError(null)
    setValidationErrors([])
    setIsCreating(false)
    setIsEditing(false)
  }

  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">Equipment profiles</FieldLegend>

        <div className="mt-3 space-y-4">
          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="outline" className="gap-2" onClick={handleNew}>
              <Plus className="h-4 w-4" />
              New profile
            </Button>
            <Button type="button" variant="outline" className="gap-2" onClick={handleUploadClick} disabled={uploading || saving || deleting}>
              <Upload className="h-4 w-4" />
              {uploading ? 'Uploading...' : 'Upload'}
            </Button>
            <input
              ref={uploadInputRef}
              type="file"
              accept=".json,application/json"
              className="sr-only"
              aria-label="Upload equipment profile file"
              onChange={(event) => { void handleUploadChange(event) }}
            />
          </div>

          {profiles.length > 0 && (
            <Field>
              <FieldLabel htmlFor="equipment-profile-select">Profile</FieldLabel>
              <select
                id="equipment-profile-select"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={selectedID}
                onChange={(event) => setSelectedID(event.target.value)}
                disabled={isEditing || saving || deleting}
              >
                {profiles.map((item) => (
                  <option key={item.id} value={item.id}>{item.name}</option>
                ))}
              </select>
            </Field>
          )}

          {isEditing && (
            <div className="flex flex-wrap gap-2">
              <Button type="button" onClick={() => { void handleSave() }} disabled={saving}>
                {saving ? 'Saving...' : 'Save changes'}
              </Button>
              <Button type="button" variant="outline" onClick={handleCancel} disabled={saving || deleting}>Cancel</Button>
            </div>
          )}

          {jsonError && (
            <p className="rounded-md border border-destructive/40 bg-destructive/10 p-2 text-sm text-destructive" role="alert">
              {jsonError}
            </p>
          )}

          {validationErrors.length > 0 && (
            <FieldError
              className="rounded-md border border-destructive/40 bg-destructive/10 p-2"
              errors={validationErrors.map((entry) => ({
                message: entry.path ? `${entry.path}: ${entry.message}` : entry.message,
              }))}
            />
          )}

          {error && (
            <p className="rounded-md border border-destructive/40 bg-destructive/10 p-2 text-sm text-destructive" role="alert">
              {error}
            </p>
          )}

          <div className="rounded-md border border-border bg-card/50 p-3">
            <div className="flex items-center justify-between gap-3">
              <div>
                <p className="text-sm font-medium">{profile.name}</p>
                <p className="text-[11px] text-muted-foreground">{profile.file} · {profile.kind}</p>
              </div>
              <div className="flex items-center gap-2">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="gap-2"
                  onClick={handleEdit}
                  disabled={isEditing || loading || profiles.length === 0 || deleting}
                >
                  <PencilLine className="h-4 w-4" />
                  Edit
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="gap-2"
                  aria-label="Download equipment profile"
                  onClick={handleDownload}
                  disabled={isCreating || deleting || !profile.id}
                >
                  <Download className="h-4 w-4" />
                  Download
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="gap-2 text-destructive"
                  aria-label="Delete equipment profile"
                  onClick={() => { void handleDelete() }}
                  disabled={isCreating || deleting || loading || profiles.length === 0}
                >
                  <Trash2 className="h-4 w-4" />
                  {deleting ? 'Deleting...' : 'Delete'}
                </Button>
              </div>
            </div>
          </div>

          {isCreating && (
            <Field>
              <FieldLabel htmlFor="equipment-profile-kind">Profile kind</FieldLabel>
              <select
                id="equipment-profile-kind"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={profile.kind}
                onChange={(event) => handleNewKindChange(event.target.value as ProfileKind)}
                disabled={saving}
              >
                <option value="engine">Engine</option>
                <option value="generator">Generator</option>
              </select>
              <FieldDescription>
                Pick a kind to start from an explicit template before editing JSON.
              </FieldDescription>
            </Field>
          )}

          <Field>
            <FieldLabel htmlFor="equipment-profile-name">Profile name</FieldLabel>
            <Input
              id="equipment-profile-name"
              value={profile.name}
              readOnly={!isEditing}
              onChange={(event) => setProfile((current) => ({ ...current, name: event.target.value }))}
              aria-invalid={jsonError !== null ? 'true' : undefined}
            />
            <FieldDescription>
              Upload or create a JSON profile, then apply it from the dashboard’s “From engine profile…” action.
            </FieldDescription>
          </Field>

          <Field>
            <FieldLabel htmlFor="equipment-profile-json">Profile JSON</FieldLabel>
            <textarea
              id="equipment-profile-json"
              rows={12}
              value={profile.json}
              readOnly={!isEditing}
              onChange={(event) => {
                setJsonError(null)
                setProfile((current) => ({ ...current, json: event.target.value }))
              }}
              aria-invalid={jsonError !== null ? 'true' : undefined}
              aria-errormessage={jsonError ? 'equipment-profile-json-error' : undefined}
              className="min-h-[220px] w-full rounded-md border bg-background px-3 py-2 text-sm text-foreground resize-y"
            />
            {jsonError && (
              <FieldDescription id="equipment-profile-json-error" className="text-destructive">
                {jsonError}
              </FieldDescription>
            )}
          </Field>
        </div>
      </FieldSet>
    </div>
  )
}
