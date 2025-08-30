//go:build linux && wayland
// +build linux,wayland

#include "keycode.h"

struct XSpecialCharacterMapping XSpecialCharacterTable[] = {
    {'~', XKB_KEY_asciitilde},
    {'_', XKB_KEY_underscore},
    {'[', XKB_KEY_bracketleft},
    {']', XKB_KEY_bracketright},
    {'!', XKB_KEY_exclam},
    {'#', XKB_KEY_numbersign},
    {'$', XKB_KEY_dollar},
    {'%', XKB_KEY_percent},
    {'&', XKB_KEY_ampersand},
    {'*', XKB_KEY_asterisk},
    {'+', XKB_KEY_plus},
    {',', XKB_KEY_comma},
    {'-', XKB_KEY_minus},
    {'.', XKB_KEY_period},
    {'?', XKB_KEY_question},
    {'<', XKB_KEY_less},
    {'>', XKB_KEY_greater},
    {'=', XKB_KEY_equal},
    {'@', XKB_KEY_at},
    {':', XKB_KEY_colon},
    {';', XKB_KEY_semicolon},
    {'{', XKB_KEY_braceleft},
    {'}', XKB_KEY_braceright},
    {'|', XKB_KEY_bar},
    {'^', XKB_KEY_asciicircum},
    {'(', XKB_KEY_parenleft},
    {')', XKB_KEY_parenright},
    {' ', XKB_KEY_space},
    {'/', XKB_KEY_slash},
    {'\\', XKB_KEY_backslash},
    {'`', XKB_KEY_grave},
    {'"', XKB_KEY_quoteright},
    {'\'', XKB_KEY_quotedbl},
    {'\t', XKB_KEY_Tab},
    {'\n', XKB_KEY_Return}
};
