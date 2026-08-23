#!/usr/bin/env python3
"""Flatten 4CLOP's content tables into one JSON file an LLM can read safely.

`clop/tables with data.sql` is authoritative but misleading to read directly:
costs live in key/value rows rather than columns, `satisfaction` means two
different things in two different tables, and several building descriptions
contradict their own numbers. Anything reading the raw dump gets at least one
of those wrong.

This script joins the five content tables into one record per building, per
action, and per good, with those traps resolved and named. It also carries the
tick rules that exist only as PHP if-chains in `clop/cron/frequent.php` and
`clop/backend/allfunctions.php` -- there is no table for those.

Usage:  python3 tools/export_gamedata.py [--check]

Writes gamedata.json next to the repo root. --check verifies the existing file
is up to date without rewriting it (exit 1 if stale).
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any, Dict, List, Optional

sys.path.insert(0, str(Path(__file__).resolve().parent))

from sqlparse_lite import group_by, index_by, parse_dump  # noqa: E402

REPO_ROOT = Path(__file__).resolve().parent.parent
DUMP_PATH = REPO_ROOT / "clop" / "tables with data.sql"
OUTPUT_PATH = REPO_ROOT / "gamedata.json"

# --------------------------------------------------------------------------
# Static reference: things that exist only as PHP control flow, not as data.
# Every entry cites the file and line it was read from so it can be rechecked
# when the game changes.
# --------------------------------------------------------------------------

REGIONS = {
    0: "Any",
    1: "Saddle Arabia",
    2: "Zebrica",
    3: "Burrozil",
    4: "Przewalskia",
}
SUBREGIONS = {0: "Any", 1: "North", 2: "Central", 3: "South"}
PRZEWALSKIA_REGION = 4

FORCE_TYPES = {1: "Cavalry", 2: "Tanks", 3: "Pegasi", 4: "Unicorns", 5: "Naval", 6: "Alicorns"}

# backend/backend_createforces.php:22-54 (hire) and cron/frequent.php:378-478 (upkeep).
# Both are per point of force size, and upkeep is charged on war ticks only.
UNITS = {
    "Cavalry":  {"type_id": 1, "hire_cost_per_size": 200000,  "upkeep_per_size": {"Apples": 5}},
    "Tanks":    {"type_id": 2, "hire_cost_per_size": 300000,  "upkeep_per_size": {"Gasoline": 5}},
    "Pegasi":   {"type_id": 3, "hire_cost_per_size": 300000,  "upkeep_per_size": {"Coffee": 5}},
    "Unicorns": {"type_id": 4, "hire_cost_per_size": 400000,  "upkeep_per_size": {"Gems": 5}},
    "Naval":    {"type_id": 5, "hire_cost_per_size": 250000,  "upkeep_per_size": {"Gasoline": 5}},
    "Alicorns": {"type_id": 6, "hire_cost_per_size": 2000000, "upkeep_per_size": {"Gems": 10}},
}

# cron/frequent.php:757-771. The PHP writes `$gdp += $gdp * N`, so the total
# factor is N+1 -- recording both halves because that += is easy to misread.
GOVERNMENTS = {
    "Loose Despotism": {"gdp_multiplier": None, "satisfaction_cap": 1000, "upkeep_per_tick": {}},
    "Democracy": {
        "gdp_multiplier": None, "satisfaction_cap": 1500,
        "upkeep_per_tick": {"Gasoline": 20, "Vehicle Parts": 2},
        "satisfaction_if_paid": 15, "satisfaction_if_unpaid": -20,
        "relations_per_tick": {"Solar Empire": -3, "New Lunar Republic": 2},
    },
    "Repression": {
        "gdp_multiplier": 2.0, "satisfaction_cap": 1000,
        "upkeep_per_tick": {"Gasoline": 10}, "satisfaction_if_unpaid": -50,
        "relations_per_tick": {"Solar Empire": 2, "New Lunar Republic": -3},
    },
    "Independence": {
        "gdp_multiplier": None, "satisfaction_cap": 2500,
        "upkeep_per_tick": {"Gasoline": 40, "Vehicle Parts": 4},
        "satisfaction_if_paid": 50, "satisfaction_if_unpaid": -100,
        "relations_per_tick": {"Solar Empire": -3, "New Lunar Republic": 6},
    },
    "Decentralization": {
        "gdp_multiplier": None, "satisfaction_cap": 2000,
        "upkeep_per_tick": {"Gasoline": 50, "Vehicle Parts": 5},
        "satisfaction_if_paid": 30, "satisfaction_if_unpaid": -100,
        "relations_per_tick": {"Solar Empire": -3, "New Lunar Republic": 4},
    },
    "Authoritarianism": {
        "gdp_multiplier": 2.5, "satisfaction_cap": 1000,
        "upkeep_per_tick": {"Gasoline": 10, "Machinery Parts": 3},
        "satisfaction_if_unpaid": -400,
        "relations_per_tick": {"Solar Empire": 4, "New Lunar Republic": -3},
    },
    "Oppression": {
        "gdp_multiplier": 3.0, "satisfaction_cap": 1000,
        "upkeep_per_tick": {"Gasoline": 10, "Machinery Parts": 5},
        "satisfaction_if_unpaid": -500,
        "relations_per_tick": {"Solar Empire": 6, "New Lunar Republic": -3},
        "can_use_market": False,
    },
    "Solar Vassal": {
        "gdp_multiplier": None, "satisfaction_cap": 1250, "upkeep_per_tick": {},
        "relations_per_tick": {"Solar Empire": 60},
    },
    "Lunar Client": {
        "gdp_multiplier": None, "satisfaction_cap": 1250, "upkeep_per_tick": {},
        "relations_per_tick": {"New Lunar Republic": 60},
    },
    "Alicorn Elite": {
        "gdp_multiplier": 6.0, "satisfaction_cap": 5000, "upkeep_per_tick": {},
        "can_build_alicorns": True,
    },
    "Transponyism": {
        "gdp_multiplier": 8.0, "satisfaction_cap": 7000, "upkeep_per_tick": {},
        "can_build_alicorns": True,
    },
}

# cron/frequent.php:613-644 and backend/allfunctions.php:219-249.
ECONOMIES = {
    "Poorly Defined": {
        "upkeep_per_tick": {}, "buy_multiplier": 1.10, "sell_multiplier": 0.90,
    },
    "State Controlled": {
        "upkeep_per_tick": {"Cider": 6}, "satisfaction_if_unpaid": -25,
        "buy_multiplier": 1.20, "sell_multiplier": 0.85,
        "relations_per_tick": {"Solar Empire": 1, "New Lunar Republic": -3},
    },
    "Free Market": {
        "upkeep_per_tick": {"Coffee": 6}, "satisfaction_if_unpaid": -25,
        "buy_multiplier": 1.03, "sell_multiplier": 0.97,
        "relations_per_tick": {"Solar Empire": -3, "New Lunar Republic": 1},
    },
}

NATIONS = {
    "Saddle Arabia": {"region_id": 1, "produces_exclusively": ["Oil"]},
    "Zebrica": {"region_id": 2, "produces_exclusively": ["Copper", "Tungsten", "Gems"]},
    "Burrozil": {"region_id": 3, "produces_exclusively": ["Apples", "Coffee", "Drugs"]},
    "Przewalskia": {
        "region_id": 4,
        "produces_exclusively": [],
        "note": (
            "Produces none of the region-locked raw goods and can never build "
            "their extractors. In exchange it pays cost_przewalskia -- a much "
            "lower money cost -- on everything it can build. Hard mode."
        ),
    },
}

RULES = {
    "ticks": {
        "interval_hours": 2,
        "war_tick_hours_utc": [0, 12],
        "war_tick_note": (
            "A war tick is an ordinary tick that also resolves combat. Military "
            "upkeep is charged ONLY on war ticks, forces only arrive on war ticks, "
            "and conquest only happens on a war tick. On non-war ticks armies do "
            "nothing and cost nothing."
        ),
        "hour_0_only": ["force training +1 (capped at 20)", "nation age +1"],
        "source": "cron/frequent.php:1043-1046, :378, :1604-1613",
    },
    "production": {
        "formula": "produced_per_tick = effect_amount * effective_count",
        "effective_count": "resources.amount - resources.disabled",
        "modifiers": "NONE. There is no tech, labor, or pollution scaling on output.",
        "all_or_nothing": (
            "If ANY per-tick input is short for the WHOLE stack of a building type, "
            "the entire stack produces nothing that tick and you lose 1 satisfaction "
            "per building. It is not partial or pro-rata."
        ),
        "same_tick_note": (
            "Goods produced during a tick are written straight to the database and "
            "are NOT available to feed another building in that same tick."
        ),
        "disabled_note": "Each disabled building costs 1 satisfaction per tick.",
        "source": "cron/frequent.php:259-340",
    },
    "pollution": {
        "formula": "satisfaction_loss = ceil(max(0, effective_count - bad_min) ** 2 / bad_div)",
        "applies_when": "bad_min is non-zero and effective_count > bad_min",
        "per": "Computed separately per building type, then summed.",
        "mitigation": (
            "Total pollution across all building types is multiplied by 0.9 ** n, "
            "where n is the number of WORKING Solar/Lunar Environmental Facilities "
            "(they must have their 5 Energy per tick to count). The difference is "
            "refunded as satisfaction."
        ),
        "source": "cron/frequent.php:277-283, :350-356",
    },
    "gdp": {
        "formula": "gdp = (50000 + sum(building.gdp_per_tick * working_count)) * multiplier",
        "multiplier": (
            "Fixed per government where governments[g].gdp_multiplier is set; "
            "otherwise 1 + (satisfaction / 1000)."
        ),
        "base": 50000,
        "caveat": (
            "The in-game estimate in backend/allfunctions.php:113-117 sums gdp over "
            "ALL owned buildings regardless of whether their inputs are available, "
            "so it over-reports. Only working buildings actually pay out."
        ),
        "source": "cron/frequent.php:101, :321, :757-802",
    },
    "combat": {
        "damage_per_hit": (
            "round(weapon.dmg_<defender_type> * armor.arm_<attacker_type> "
            "* 1.5 ** ((attacker_training - defender_training) / 20), 3)"
        ),
        "crossed_indexing_warning": (
            "The attacker's weapon column is chosen by the DEFENDER's type, and the "
            "defender's armor column is chosen by the ATTACKER's type. Getting this "
            "backwards is the single easiest mistake to make here."
        ),
        "armor_direction": "arm_* is a damage MULTIPLIER. Lower is better.",
        "home_defence_bonus": (
            "Damage against a unit defending its own nation is multiplied by 0.75, "
            "unless that nation's owner is in stasis mode."
        ),
        "hits_per_unit": "A unit lands `size` hits.",
        "casualties": "floor(round(accumulated_damage, 6)); at or above `size` the force is destroyed.",
        "unequipped_defaults": {
            "no_weapon": "dmg 0.25 against every type",
            "no_armor": "arm 1.0 against every type (no reduction)",
            "note": (
                "Weapon/armor id 0 is 'Scrounged'. PHP treats id 0 as falsy, so "
                "Scrounged gear takes these fallbacks rather than any table row."
            ),
        },
        "alicorns": (
            "Force type 6 is recast as type 5 (Naval) for targeting, with a flat "
            "dmg of 10 and arm of 0.1 against every type. Equipped weapons and "
            "armor are ignored entirely."
        ),
        "training": (
            "National recruits arrive at training = min(Barracks owned, 20). "
            "Mercenaries arrive at training 10 and cost 2x-3x depending on relation."
        ),
        "source": "cron/frequent.php:1146-1167, :1232, :1394-1408",
    },
    "market": {
        "price_is": "Per unit, always.",
        "order_book": (
            "marketplace = sell offers, buyermarketplace = standing bids. The unique "
            "key is (nation_id, resource_id, price), so one nation can hold several "
            "rows for the same good at different prices."
        ),
        "spread": (
            "The buyer pays price * buy_multiplier; the seller receives "
            "price * sell_multiplier. The difference is destroyed, not transferred."
        ),
        "buyer_limits": (
            "Minimum bid price 1000. Total outstanding bid value is capped at "
            "200,000,000 + funds/10 for resources, 100,000,000 + funds/10 for gear."
        ),
        "source": "backend/backend_marketplace.php:183-199, backend/allfunctions.php:219-249",
    },
    "stockpile_siphon": {
        "goods": "Anything over 50,000 loses ceil((amount - 50000) / 500) per tick.",
        "equipment": "Weapons/armor over 1,000 lose ceil((amount - 1000) / 500) per tick.",
        "source": "cron/frequent.php:343-374",
    },
    "satisfaction_penalties": {
        "no_buildings": -5,
        "military_size": "ceil((total force size - 20) / 2) if total size > 20",
        "empire_size": (
            "(nations - 1) ** 2 * 20, divided by 3 under Oppression, "
            "Alicorn Elite, or Transponyism"
        ),
        "death": "Below -5000 satisfaction the nation is deleted into the graveyard.",
        "rebels": (
            "Below a per-government threshold, rebels (nation_id -3) spawn with "
            "size ceil(-satisfaction / 4) minus a government bonus."
        ),
        "source": "cron/frequent.php:156-163, :682-698, :806-960",
    },
}

# Resources 76 and 77 have no `recipes` row -- they are inserted directly by
# backend/backend_majoractions.php:128 as part of the ascension chain.
NO_RECIPE_NOTE = (
    "Has no recipes row. Created by the ascension flow in "
    "backend/backend_majoractions.php:128, not by a normal build action."
)

# Verified by reading the rows against their own columns; see _meta.warnings.
DESCRIPTION_MISMATCHES = {
    "Mall": "Its resourcedef description claims 400,000 GDP. The gdp column says 250,000, and the build recipe's description agrees with the column.",
    "Bar": "Its description says '3 coffee and 1 pie'. It actually consumes 3 Cider and 1 Pie.",
    "Video Arcade": "Its resourcedef description says 6 energy. It actually consumes 5, and the build recipe's description agrees with the column.",
}

DECIMAL_COLUMNS = (
    "dmg_cavalry", "dmg_tanks", "dmg_pegasi", "dmg_unicorns", "dmg_naval",
    "arm_cavalry", "arm_tanks", "arm_pegasi", "arm_unicorns", "arm_naval",
)


def _floats(row: Dict[str, Any]) -> Dict[str, Any]:
    """MySQL DECIMAL columns come out of the dump quoted; make them numbers."""
    out = dict(row)
    for col in DECIMAL_COLUMNS:
        if col in out and out[col] is not None:
            out[col] = float(out[col])
    return out


def _amounts(rows: List[Dict[str, Any]], key: str, names: Dict[int, str]) -> Dict[str, int]:
    """Turn key/value rows into a {good name: amount} mapping."""
    out: Dict[str, int] = {}
    for row in rows:
        name = names.get(row[key])
        if name is None:
            continue
        out[name] = out.get(name, 0) + row["amount"]
    return out


def _recipe_record(recipe: Dict[str, Any], items: List[Dict[str, Any]],
                   names: Dict[int, str]) -> Dict[str, Any]:
    """One build action, with the two cost mechanisms kept apart."""
    region = recipe["region"]
    # cost_przewalskia is only meaningful where Przewalskia can actually build.
    # For a recipe locked to another region the stored 0 is dead data, not free.
    przewalskia_buildable = region in (0, PRZEWALSKIA_REGION)

    consumed = [i for i in items if i["is_used_up"]]
    required = [i for i in items if not i["is_used_up"]]

    record = {
        "recipe_id": recipe["recipe_id"],
        "action_name": recipe["name"],
        "money_cost": recipe["cost"],
        "money_cost_przewalskia": recipe["cost_przewalskia"] if przewalskia_buildable else None,
        "build_consumes": _amounts(consumed, "resource_id", names),
        "build_requires_owned": _amounts(required, "resource_id", names),
        "produces_amount": recipe["amount"],
        "region": REGIONS.get(region, str(region)),
        "subregion": SUBREGIONS.get(recipe["subregion"], str(recipe["subregion"])),
        "satisfaction_on_build": recipe["satisfaction"],
        "se_relation_on_build": recipe["se_relation"],
        "nlr_relation_on_build": recipe["nlr_relation"],
        "secret": bool(recipe["secret"]),
        "description_prose_unverified": recipe["description"],
    }
    if not przewalskia_buildable:
        record["przewalskia_note"] = (
            f"Przewalskia cannot build this at all -- it is locked to "
            f"{REGIONS.get(region, region)}."
        )
    if required:
        record["build_requires_owned_note"] = (
            "These are prerequisites that must be OWNED. They are checked but "
            "NOT consumed by the build."
        )
    return record


def build_export(dump_text: str) -> Dict[str, Any]:
    tables = parse_dump(dump_text)

    resourcedefs = tables["resourcedefs"]
    names = {r["resource_id"]: r["name"] for r in resourcedefs}
    groups = {g["recipegroup_id"]: g["name"] for g in tables["recipegroups"]}

    recipes = tables["recipes"]
    recipe_ids = {r["recipe_id"] for r in recipes}
    items_by_recipe = group_by(tables["recipeitems"], "recipe_id")
    effects_by_source = group_by(tables["resourceeffects"], "resource_id")
    reqs_by_source = group_by(tables["resourcerequirements"], "resource_id")
    recipes_by_output = group_by(recipes, "resource_id")

    orphan_recipe_ids = sorted(set(items_by_recipe) - recipe_ids)

    # DNA facilities all share one name and are told apart only by region.
    name_counts: Dict[str, int] = {}
    for recipe in recipes:
        name_counts[recipe["name"]] = name_counts.get(recipe["name"], 0) + 1

    def display(recipe: Dict[str, Any]) -> str:
        if name_counts[recipe["name"]] == 1:
            return recipe["name"]
        where = " ".join(
            part for part in (SUBREGIONS.get(recipe["subregion"]), REGIONS.get(recipe["region"]))
            if part and part != "Any"
        )
        return f"{recipe['name']} ({where})" if where else recipe["name"]

    buildings: List[Dict[str, Any]] = []
    goods: List[Dict[str, Any]] = []

    for rd in sorted(resourcedefs, key=lambda r: r["name"]):
        rid = rd["resource_id"]
        produces = _amounts(effects_by_source.get(rid, []), "affectedresource_id", names)
        consumes = _amounts(reqs_by_source.get(rid, []), "requiredresource_id", names)

        common = {
            "resource_id": rid,
            "name": rd["name"],
            "description_prose_unverified": rd["description"],
        }
        if rd["name"] in DESCRIPTION_MISMATCHES:
            common["description_warning"] = DESCRIPTION_MISMATCHES[rd["name"]]

        if rd["is_building"]:
            built_by = [
                _recipe_record(r, items_by_recipe.get(r["recipe_id"], []), names)
                for r in sorted(recipes_by_output.get(rid, []), key=lambda r: r["recipe_id"])
            ]
            for record, recipe in zip(built_by, sorted(recipes_by_output.get(rid, []),
                                                      key=lambda r: r["recipe_id"])):
                record["action_display_name"] = display(recipe)
                record["group"] = groups.get(recipe["recipegroup_id"], "")

            record = dict(common)
            record.update({
                "produces_per_tick": produces,
                "consumes_per_tick": consumes,
                "satisfaction_per_tick": rd["satisfaction"],
                "gdp_per_tick": rd["gdp"],
                "se_relation_per_tick": rd["se_relation"],
                "nlr_relation_per_tick": rd["nlr_relation"],
                "satisfaction_on_destroy": rd["satisfaction_on_destroy"],
                "pollution": {
                    "bad_min": rd["bad_min"],
                    "bad_div": rd["bad_div"],
                    "pollutes": bool(rd["bad_min"]),
                    "formula": RULES["pollution"]["formula"],
                },
                "built_by": built_by,
            })
            if not built_by:
                record["no_recipe_note"] = NO_RECIPE_NOTE
            buildings.append(record)
        else:
            record = dict(common)
            record.update({
                "tradeable": bool(rd["is_tradeable"]),
                "satisfaction_per_tick_per_unit": rd["satisfaction"],
                "produced_by": sorted(
                    {names[src] for src, rows in effects_by_source.items()
                     for row in rows if row["affectedresource_id"] == rid
                     if src in names}
                ),
                "consumed_by": sorted(
                    {names[src] for src, rows in reqs_by_source.items()
                     for row in rows if row["requiredresource_id"] == rid
                     if src in names}
                ),
            })
            goods.append(record)

    # Recipes whose output is not a building, plus the ones that produce nothing
    # at all (pure satisfaction or relation actions).
    building_ids = {r["resource_id"] for r in resourcedefs if r["is_building"]}
    actions = []
    for recipe in sorted(recipes, key=lambda r: r["recipe_id"]):
        if recipe["resource_id"] in building_ids:
            continue
        record = _recipe_record(recipe, items_by_recipe.get(recipe["recipe_id"], []), names)
        record["action_display_name"] = display(recipe)
        record["group"] = groups.get(recipe["recipegroup_id"], "")
        record["produces"] = names.get(recipe["resource_id"]) if recipe["resource_id"] else None
        actions.append(record)

    def gear(defs_table: str, recipes_table: str, items_table: str,
             id_col: str, stat_prefix: str) -> List[Dict[str, Any]]:
        defs = index_by(tables[defs_table], id_col)
        gear_recipes = tables[recipes_table]
        gear_items = group_by(tables[items_table], f"{recipes_table[:-7]}recipe_id")
        by_output = group_by(gear_recipes, id_col)

        out = []
        for gear_id, row in sorted(defs.items()):
            row = _floats(row)
            record = {
                id_col: gear_id,
                "name": row["name"],
                "for_force_type": FORCE_TYPES.get(row["type"], str(row["type"])),
                "stats": {k[len(stat_prefix):]: v for k, v in row.items()
                          if k.startswith(stat_prefix)},
            }
            if "carrying" in row:
                record["carrying_capacity"] = row["carrying"]
            for recipe in by_output.get(gear_id, []):
                key = f"{recipes_table[:-1]}_id"
                items = gear_items.get(recipe[key], [])
                record["build"] = {
                    "money_cost": recipe["cost"],
                    "amount": recipe["amount"],
                    "consumes": _amounts([i for i in items if i["is_used_up"]],
                                         "resource_id", names),
                    "requires_owned": _amounts([i for i in items if not i["is_used_up"]],
                                               "resource_id", names),
                }
            out.append(record)
        return out

    weapons = gear("weapondefs", "weaponrecipes", "weaponrecipeitems", "weapon_id", "dmg_")
    armor = gear("armordefs", "armorrecipes", "armorrecipeitems", "armor_id", "arm_")

    warnings = [
        "Every `description_prose_unverified` field is the game's own prose and is "
        "NOT reliable. Where prose and columns disagree, the columns are what the "
        "tick actually does. Three known contradictions are flagged inline with a "
        "`description_warning` field.",
        "`satisfaction_on_build` (a one-time payout when you build) and "
        "`satisfaction_per_tick` (an ongoing effect) are stored in the same-named "
        "column of two different tables. They are not the same number and are not "
        "interchangeable.",
        "`build_requires_owned` lists prerequisites that are checked but NOT "
        "consumed. Only `build_consumes` is spent.",
        "`money_cost_przewalskia` is null where Przewalskia cannot build the thing "
        "at all. The raw table stores 0 there, which does not mean free.",
    ]
    if orphan_recipe_ids:
        warnings.append(
            f"Dropped orphan recipeitems rows for recipe_id(s) "
            f"{', '.join(str(i) for i in orphan_recipe_ids)}: they have cost rows "
            f"but no matching recipe, so they are unbuildable dead data."
        )

    return {
        "_meta": {
            "generated_from": "clop/tables with data.sql",
            "generator": "tools/export_gamedata.py",
            "regenerate_with": "python3 tools/export_gamedata.py",
            "warnings": warnings,
            "counts": {
                "buildings": len(buildings),
                "goods": len(goods),
                "actions": len(actions),
                "weapons": len(weapons),
                "armor": len(armor),
            },
        },
        "buildings": buildings,
        "goods": goods,
        "actions": actions,
        "units": UNITS,
        "weapons": weapons,
        "armor": armor,
        "governments": GOVERNMENTS,
        "economies": ECONOMIES,
        "nations": NATIONS,
        "regions": {str(k): v for k, v in REGIONS.items()},
        "subregions": {str(k): v for k, v in SUBREGIONS.items()},
        "force_types": {str(k): v for k, v in FORCE_TYPES.items()},
        "rules": RULES,
    }


def main(argv: Optional[List[str]] = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dump", type=Path, default=DUMP_PATH)
    parser.add_argument("--out", type=Path, default=OUTPUT_PATH)
    parser.add_argument("--check", action="store_true",
                        help="verify the committed file is current; do not write")
    args = parser.parse_args(argv)

    if not args.dump.exists():
        print(f"error: {args.dump} not found", file=sys.stderr)
        return 2

    export = build_export(args.dump.read_text(encoding="utf-8"))
    rendered = json.dumps(export, indent=2, ensure_ascii=False) + "\n"

    if args.check:
        if not args.out.exists():
            print(f"error: {args.out} does not exist", file=sys.stderr)
            return 1
        if args.out.read_text(encoding="utf-8") != rendered:
            print(f"error: {args.out} is stale; rerun without --check", file=sys.stderr)
            return 1
        print(f"{args.out.name} is up to date")
        return 0

    args.out.write_text(rendered, encoding="utf-8")
    counts = export["_meta"]["counts"]
    print(f"wrote {args.out} -- " + ", ".join(f"{v} {k}" for k, v in counts.items()))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
