package main

import (
	"fmt"
	"strings"
	"time"
)

// The boat's particulars as a registered record type. They are one record,
// not a collection, so the type has a single fixed id and no create or
// delete: Mate can fill in what the operator has not yet entered, never make
// a second record or remove the one that exists. An update goes through the
// same validation and the same write the Settings page uses.

const (
	recordTypeVesselParticulars = "vessel_particulars"
	vesselParticularsRecordID   = "vessel"
	// vesselParticularsUnsetVersion is the version of a record nothing has
	// been saved to yet, so a changeset written against the empty record is
	// still checked for freshness when it is applied.
	vesselParticularsUnsetVersion = "unset"
)

func init() {
	defaultRecordRegistry.register(vesselParticularsRecordType())
}

func vesselParticularsRecordType() *recordType {
	str := func(name, desc string) recordField {
		return recordField{Name: name, Kind: kindString, Writable: true, Description: desc}
	}
	return &recordType{
		Name:  recordTypeVesselParticulars,
		Label: "vessel details",
		Summary: "The one record of facts about the boat that live instrument data does not carry: owner and insurance, " +
			"home marina and berth, hull and registration. There is exactly one, with the id \"vessel\"; it can be updated, never created or deleted. " +
			"Name, call sign, MMSI and draft are not here (the boat's instruments supply them). " +
			"Text fields are cleared with an empty string.",
		Actions: []string{changeUpdate},
		Fields: []recordField{
			str("owner_name", "the owner's full name, as it should appear on forms"),
			str("owner_phone", "the owner's phone number"),
			str("owner_email", "the owner's email address"),
			str("insurer", "the insurance company"),
			str("policy_number", "the insurance policy number"),
			str("home_marina", "the marina the boat is normally kept in"),
			str("berth", "the berth or pen number at the home marina"),
			str("storm_delegate", "the person who prepares the boat when the owner is away, with how to reach them"),
			{Name: "loa_m", Kind: kindNumber, Writable: true, Nullable: true, Description: "length overall in metres; null clears it"},
			{Name: "beam_m", Kind: kindNumber, Writable: true, Nullable: true, Description: "beam in metres; null clears it"},
			str("builder", "the builder"),
			str("model", "the model"),
			{Name: "year", Kind: kindInteger, Writable: true, Nullable: true, Description: "year built; null clears it"},
			str("hin", "hull identification number"),
			str("flag", "flag state"),
			str("hailing_port", "hailing port"),
			str("hull_type", "hull type, such as catamaran or monohull"),
			str("hull_material", "hull material"),
			{Name: "displacement_kg", Kind: kindNumber, Writable: true, Nullable: true, Description: "displacement in kilograms; null clears it"},
			str("shore_power", "shore power arrangement"),
			str("system_voltage", "system voltage"),
			str("registration", "registration number"),
			str("imo", "IMO number"),
			str("epirb_id", "EPIRB beacon ID"),
			{Name: "date_acquired", Kind: kindDate, Writable: true, Description: "when the owner acquired the boat, YYYY-MM-DD; empty clears it"},
		},
		Get:      getVesselParticularsRecord,
		List:     listVesselParticularsRecord,
		Update:   updateVesselParticularsRecord,
		Describe: describeVesselParticulars,
		Href:     func(recordSnapshot) string { return "/settings/boat-ui" },
	}
}

func vesselParticularsSnapshot(v vesselParticulars) recordSnapshot {
	version := vesselParticularsUnsetVersion
	if v.UpdatedAt != nil {
		version = v.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return recordSnapshot{
		ID:      vesselParticularsRecordID,
		Label:   "Vessel details",
		Version: version,
		Fields: map[string]any{
			"owner_name":      v.OwnerName,
			"owner_phone":     v.OwnerPhone,
			"owner_email":     v.OwnerEmail,
			"insurer":         v.Insurer,
			"policy_number":   v.PolicyNumber,
			"home_marina":     v.HomeMarina,
			"berth":           v.Berth,
			"storm_delegate":  v.StormDelegate,
			"loa_m":           nilFloat(v.LOAM),
			"beam_m":          nilFloat(v.BeamM),
			"builder":         v.Builder,
			"model":           v.Model,
			"year":            nilInt(v.Year),
			"hin":             v.HIN,
			"flag":            v.Flag,
			"hailing_port":    v.HailingPort,
			"hull_type":       v.HullType,
			"hull_material":   v.HullMaterial,
			"displacement_kg": nilFloat(v.DisplacementKG),
			"shore_power":     v.ShorePower,
			"system_voltage":  v.SystemVoltage,
			"registration":    v.Registration,
			"imo":             v.IMO,
			"epirb_id":        v.EPIRBID,
			"date_acquired":   v.DateAcquired,
		},
	}
}

func getVesselParticularsRecord(q sqlQueryer, id string) (recordSnapshot, error) {
	if id != vesselParticularsRecordID {
		return recordSnapshot{}, errRecordNotFound
	}
	v, err := vesselParticularsFrom(q)
	if err != nil {
		return recordSnapshot{}, err
	}
	return vesselParticularsSnapshot(v), nil
}

func listVesselParticularsRecord(s *documentStore, _ map[string]string) ([]recordSnapshot, error) {
	v, err := s.GetVesselParticulars()
	if err != nil {
		return nil, err
	}
	return []recordSnapshot{vesselParticularsSnapshot(v)}, nil
}

// applyParticularsFields lays the given fields over v. Every value has
// already passed the changeset layer's kind check; the particulars' own
// validation runs after, on the merged record.
func applyParticularsFields(v vesselParticulars, given fieldSet) vesselParticulars {
	text := map[string]*string{
		"owner_name": &v.OwnerName, "owner_phone": &v.OwnerPhone, "owner_email": &v.OwnerEmail,
		"insurer": &v.Insurer, "policy_number": &v.PolicyNumber, "home_marina": &v.HomeMarina,
		"berth": &v.Berth, "storm_delegate": &v.StormDelegate, "builder": &v.Builder, "model": &v.Model,
		"hin": &v.HIN, "flag": &v.Flag, "hailing_port": &v.HailingPort, "hull_type": &v.HullType,
		"hull_material": &v.HullMaterial, "shore_power": &v.ShorePower, "system_voltage": &v.SystemVoltage,
		"registration": &v.Registration, "imo": &v.IMO, "epirb_id": &v.EPIRBID, "date_acquired": &v.DateAcquired,
	}
	for name, dst := range text {
		if val, ok := given[name]; ok {
			*dst = stringOf(val)
		}
	}
	if val, ok := given["loa_m"]; ok {
		v.LOAM = floatPtrOf(val)
	}
	if val, ok := given["beam_m"]; ok {
		v.BeamM = floatPtrOf(val)
	}
	if val, ok := given["displacement_kg"]; ok {
		v.DisplacementKG = floatPtrOf(val)
	}
	if val, ok := given["year"]; ok {
		v.Year = intPtrOf(val)
	}
	return v
}

func updateVesselParticularsRecord(env changeEnv, before recordSnapshot, given fieldSet) error {
	cur, err := vesselParticularsFrom(env.tx)
	if err != nil {
		return err
	}
	merged := trimParticulars(applyParticularsFields(cur, given))
	if verr := validateParticulars(merged); verr != nil {
		return fieldErr(verr.Field, "%s", verr.Message)
	}
	return upsertVesselParticularsTx(env.tx, merged, env.now)
}

func describeVesselParticulars(d describeInput) string {
	var parts []string
	for _, f := range d.Type.Fields {
		val, ok := d.Op.Fields[f.Name]
		if !ok {
			continue
		}
		name := strings.ReplaceAll(f.Name, "_", " ")
		if val == nil || val == "" {
			parts = append(parts, name+" cleared")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %v", name, val))
	}
	return "Save to vessel details: " + strings.Join(parts, ", ")
}
