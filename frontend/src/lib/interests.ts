export interface InterestCategory {
	name: string;
	icon: string;
	tags: string[];
}

export const categories: InterestCategory[] = [
	{
		name: 'Technology',
		icon: '\u{1F4BB}',
		tags: ['programming', 'gaming', 'ai', 'crypto', 'gadgets', 'cybersecurity']
	},
	{
		name: 'Entertainment',
		icon: '\u{1F3AC}',
		tags: ['movies', 'music', 'anime', 'books', 'tv-shows', 'podcasts', 'comics']
	},
	{
		name: 'Lifestyle',
		icon: '\u{1F33F}',
		tags: ['fitness', 'cooking', 'travel', 'fashion', 'photography', 'gardening']
	},
	{
		name: 'Sports',
		icon: '\u26BD',
		tags: ['football', 'basketball', 'cricket', 'esports', 'f1', 'mma']
	},
	{
		name: 'Creative',
		icon: '\u{1F3A8}',
		tags: ['art', 'writing', 'design', 'filmmaking', 'music-production']
	},
	{
		name: 'Academic',
		icon: '\u{1F4DA}',
		tags: ['science', 'philosophy', 'history', 'psychology', 'mathematics', 'languages']
	},
	{
		name: 'Social',
		icon: '\u{1F4AC}',
		tags: ['relationships', 'career', 'mental-health', 'parenting', 'pets']
	},
	{
		name: 'Random',
		icon: '\u{1F3B2}',
		tags: ['memes', 'conspiracy-theories', 'shower-thoughts', 'unpopular-opinions', 'debate']
	}
];

export const MIN_INTERESTS = 1;
export const MAX_INTERESTS = 5;
