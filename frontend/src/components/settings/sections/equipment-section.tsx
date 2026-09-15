import { useEffect, useMemo, useState } from 'react'
import { Upload, Download, PencilLine, Trash2, Plus } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { apiBaseUrl } from '@/config/api'
import { useEngineProfiles } from '@/hooks/use-engine-profiles'
import type { EngineProfile } from '@/lib/engine-profiles'

const defaultProfile = {
  id: 'cummins-qsb67-550',
  name: 'Cummins QSB 6.7 550',
  file: 'cummins-qsb67-550.json',
  json: `{
  "id": "cummins-qsb67-550",
  "name": "Cummins QSB 6.7 550",
  "gauge_count": 8
}`,
}

function toEditableProfile(profile: EngineProfile) {
  return {
    id: profile.id,
    name: profile.name,
    file: `${profile.id}.json`,
    json: JSON.stringify(profile, null, 2),
  }
}

export function EquipmentSection() {
  const { profiles, loading } = useEngineProfiles(true)
  const [isEditing, setIsEditing] = useState(false)
  const [profile, setProfile] = useState(defaultProfile)
  const [selectedID, setSelectedID] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [jsonError, setJsonError] = useState<string | null>(null)

  const selectedProfile = useMemo(
    () => profiles.find((item) => item.id === selectedID) ?? profiles[0],
    [profiles, selectedID],
  )

  useEffect(() => {
    if (!selectedProfile) {
      setSelectedID('')
      setProfile(defaultProfile)
      setIsEditing(false)
      return
    }

    setSelectedID(selectedProfile.id)
    setProfile(toEditableProfile(selectedProfile))
    setIsEditing(false)
    setError(null)
    setJsonError(null)
  }, [selectedProfile?.id])

  const handleEdit = () => setIsEditing(true)

  const handleSave = async () => {
    setError(null)
    setJsonError(null)

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
      const response = await fetch(`${apiBaseUrl}/api/engine-profiles/${encodeURIComponent(profile.id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...parsed, name: profile.name }),
      })

      if (!response.ok) {
        const payload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(payload?.error ?? 'Unable to save profile.')
      }

      setProfile((current) => ({
        ...current,
        json: JSON.stringify({ ...parsed, name: profile.name }, null, 2),
      }))
      setIsEditing(false)
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
    setIsEditing(false)
  }

  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">Equipment profiles</FieldLegend>

        <div className="mt-3 space-y-4">
          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="outline" className="gap-2">
              <Plus className="h-4 w-4" />
              New profile
            </Button>
            <Button type="button" variant="outline" className="gap-2">
              <Upload className="h-4 w-4" />
              Upload
            </Button>
          </div>

          {profiles.length > 0 && (
            <Field>
              <FieldLabel htmlFor="equipment-profile-select">Profile</FieldLabel>
              <select
                id="equipment-profile-select"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={selectedID}
                onChange={(event) => setSelectedID(event.target.value)}
                disabled={isEditing || saving}
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
              <Button type="button" variant="outline" onClick={handleCancel} disabled={saving}>Cancel</Button>
            </div>
          )}

          {jsonError && (
            <p className="rounded-md border border-destructive/40 bg-destructive/10 p-2 text-sm text-destructive" role="alert">
              {jsonError}
            </p>
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
                <p className="text-[11px] text-muted-foreground">{profile.file}</p>
              </div>
              <div className="flex items-center gap-2">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="gap-2"
                  onClick={handleEdit}
                  disabled={isEditing || loading || profiles.length === 0}
                >
                  <PencilLine className="h-4 w-4" />
                  Edit
                </Button>
                <Button type="button" variant="ghost" size="sm" className="gap-2" aria-label="Download equipment profile">
                  <Download className="h-4 w-4" />
                  Download
                </Button>
                <Button type="button" variant="ghost" size="sm" className="gap-2 text-destructive" aria-label="Delete equipment profile">
                  <Trash2 className="h-4 w-4" />
                  Delete
                </Button>
              </div>
            </div>
          </div>

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
