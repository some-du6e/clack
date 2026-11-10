//go:build darwin && cgo

#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

// Called on the spell service's worker thread, never the main thread.
// +[NSSpellChecker sharedSpellChecker] can throw (Chromium's
// SharedSpellChecker catches it); then spellchecking is off.

static NSSpellChecker *shared(void) {
	@try {
		return [NSSpellChecker sharedSpellChecker];
	} @catch (id e) {
		return nil;
	}
}

static NSInteger docTag = 0;

long clackSpellTag(void) {
	if (docTag == 0) {
		docTag = [NSSpellChecker uniqueSpellDocumentTag];
	}
	return (long)docTag;
}

int clackSpellReady(void) {
	@autoreleasepool {
		return shared() != nil;
	}
}

int clackSpellCheck(const char *word) {
	@autoreleasepool {
		NSSpellChecker *c = shared();
		if (c == nil) {
			return 1;
		}
		NSString *w = [NSString stringWithUTF8String:word];
		if (w == nil) {
			return 1;
		}
		NSRange r = {0, 0};
		@try {
			r = [c checkSpellingOfString:w startingAt:0 language:nil wrap:NO inSpellDocumentWithTag:clackSpellTag() wordCount:NULL];
		} @catch (id e) {
			return 1;
		}
		return r.length == 0;
	}
}

static char *dupString(NSString *s) {
	const char *u = [s UTF8String];
	if (u == NULL) {
		return NULL;
	}
	return strdup(u);
}

char *clackSpellGuesses(const char *word) {
	@autoreleasepool {
		NSSpellChecker *c = shared();
		NSString *w = [NSString stringWithUTF8String:word];
		if (c == nil || w == nil) {
			return NULL;
		}
		NSArray<NSString *> *g = nil;
		@try {
			g = [c guessesForWordRange:NSMakeRange(0, w.length) inString:w language:[c language] inSpellDocumentWithTag:clackSpellTag()];
		} @catch (id e) {
			return NULL;
		}
		NSMutableArray *out = [NSMutableArray array];
		for (NSString *s in g) {
			if (out.count >= 5) {
				break;
			}
			[out addObject:s];
		}
		return dupString([out componentsJoinedByString:@"\n"]);
	}
}

void clackSpellLearn(const char *word) {
	@autoreleasepool {
		NSSpellChecker *c = shared();
		NSString *w = [NSString stringWithUTF8String:word];
		if (c == nil || w == nil) {
			return;
		}
		@try {
			if (![c hasLearnedWord:w]) {
				[c learnWord:w];
			}
		} @catch (id e) {
		}
	}
}

char *clackSpellLocale(void) {
	@autoreleasepool {
		NSString *l = [[NSLocale preferredLanguages] firstObject];
		return l ? dupString(l) : NULL;
	}
}
