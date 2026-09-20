package main

import "testing"

func TestClassifyNoteType(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "empty body is note",
			body: "",
			want: noteTypeNote,
		},
		{
			name: "whitespace-only body is note",
			body: "   \n\t  ",
			want: noteTypeNote,
		},
		{
			name: "plain prose with no signal is note",
			body: "Remember to check on the boat next weekend sometime.",
			want: noteTypeNote,
		},
		// Ordering trap #1 (plan Verification): a procedure that mentions a
		// phone number must stay a procedure, not fall to contact.
		{
			name: "procedure containing a phone number stays procedure",
			body: "Genset shutdown:\n1. Let it cool for five minutes\n2. Close the fuel valve\n3. If it won't restart, call the yard at 555-0142.",
			want: noteTypeProcedure,
		},
		{
			name: "checklist item containing a phone number stays procedure",
			body: "- [ ] Ring Dave about the mooring, 555-0142, best reached after 4pm\n- [ ] Confirm slip number",
			want: noteTypeProcedure,
		},
		// Ordering trap #2: a recipe's quantities are unit-bearing text that
		// spec's own keyword set is built to catch, so recipe must win.
		{
			name: "recipe with units stays recipe not spec",
			body: "Passage banana bread: 2 cups flour, 500g sugar, 1 tsp cinnamon. Preheat oven to 180C and bake for 40 minutes.",
			want: noteTypeRecipe,
		},
		{
			name: "recipe keyword alone is recipe",
			body: "Ingredients: 3 eggs, a cup of milk. Whisk together and simmer.",
			want: noteTypeRecipe,
		},
		{
			name: "spec keyword without recipe signal is spec",
			body: "Cruise setting is 1850 RPM, burns about 6 gallons an hour a side. Fuel tank capacity is 400 gallons.",
			want: noteTypeSpec,
		},
		{
			name: "numbered steps without recipe signal is procedure",
			body: "1. Open the seacock\n2. Start the raw water pump\n3. Check for flow at the exhaust",
			want: noteTypeProcedure,
		},
		{
			name: "quirk keyword is quirk",
			body: "Quirk: the autopilot sometimes drops out in heavy chop, watch out for it near the bar.",
			want: noteTypeQuirk,
		},
		{
			name: "plain phone number with no other signal is contact",
			body: "Dave, yard manager - 555-0142, best reached after 4pm.",
			want: noteTypeContact,
		},
		{
			name: "email address with no other signal is contact",
			body: "Marine electrician: sparky@example.com",
			want: noteTypeContact,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyNoteType(tc.body); got != tc.want {
				t.Fatalf("classifyNoteType(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

func TestClassifyNoteType_IsDeterministic(t *testing.T) {
	body := "Genset shutdown:\n1. Let it cool\n2. Close the fuel valve. Call the yard at 555-0142 if unsure."
	first := classifyNoteType(body)
	for i := 0; i < 5; i++ {
		if got := classifyNoteType(body); got != first {
			t.Fatalf("classifyNoteType is not deterministic: call %d returned %q, first call returned %q", i, got, first)
		}
	}
}

func TestValidNoteType(t *testing.T) {
	for _, ok := range []string{noteTypeContact, noteTypeQuirk, noteTypeSpec, noteTypeProcedure, noteTypeRecipe, noteTypeNote} {
		if !validNoteType(ok) {
			t.Fatalf("expected %q to be a valid note type", ok)
		}
	}
	for _, bad := range []string{"", "unknown", "Contact", " contact"} {
		if validNoteType(bad) {
			t.Fatalf("expected %q to be an invalid note type", bad)
		}
	}
}
