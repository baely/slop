const fs = require('fs');
const path = require('path');

const csvPath = process.argv[2];
const token = process.argv[3];

const BASE = 'https://api.themoviedb.org/3';
const IMG = 'https://image.tmdb.org/t/p';

async function searchMovie(title) {
  // Extract year from title like "The Apartment (1960)"
  const match = title.match(/^(.+?)\s*\((\d{4})\)$/);
  if (!match) return null;
  const name = match[1].trim();
  const year = match[2];

  const params = new URLSearchParams({ query: name, year, include_adult: false });
  const res = await fetch(`${BASE}/search/movie?${params}`, {
    headers: { Authorization: `Bearer ${token}`, Accept: 'application/json' }
  });
  const data = await res.json();
  if (!data.results || data.results.length === 0) return null;

  const movie = data.results[0];
  return {
    id: movie.id,
    tmdb_title: movie.title,
    year,
    overview: movie.overview,
    poster: movie.poster_path ? `${IMG}/w342${movie.poster_path}` : null,
    poster_lg: movie.poster_path ? `${IMG}/w500${movie.poster_path}` : null,
    backdrop: movie.backdrop_path ? `${IMG}/w780${movie.backdrop_path}` : null,
    rating: movie.vote_average,
    release_date: movie.release_date,
  };
}

async function getDetails(movieId) {
  const res = await fetch(`${BASE}/movie/${movieId}?append_to_response=credits`, {
    headers: { Authorization: `Bearer ${token}`, Accept: 'application/json' }
  });
  const data = await res.json();
  const director = data.credits?.crew?.find(c => c.job === 'Director');
  return {
    runtime: data.runtime,
    genres: (data.genres || []).map(g => g.name),
    director: director ? director.name : null,
    tagline: data.tagline || null,
  };
}

async function main() {
  const raw = fs.readFileSync(csvPath, 'utf-8');
  const lines = raw.trim().split('\n').slice(1); // skip header

  const schedule = [];

  for (const line of lines) {
    if (!line.trim()) continue;
    // Parse CSV carefully (some fields are quoted with commas inside)
    const fields = [];
    let current = '';
    let inQuotes = false;
    for (let i = 0; i < line.length; i++) {
      if (line[i] === '"') { inQuotes = !inQuotes; continue; }
      if (line[i] === ',' && !inQuotes) { fields.push(current); current = ''; continue; }
      current += line[i];
    }
    fields.push(current);

    const [date, day, movie1, movie2, reason] = fields;
    const movies = [];

    for (const title of [movie1, movie2]) {
      if (!title || !title.trim()) continue;
      process.stderr.write(`Fetching: ${title.trim()}...\n`);
      const info = await searchMovie(title.trim());
      if (info) {
        const details = await getDetails(info.id);
        movies.push({ title: title.trim(), ...info, ...details });
      } else {
        process.stderr.write(`  WARNING: No TMDB result for "${title.trim()}"\n`);
        movies.push({ title: title.trim(), poster: null, overview: '', genres: [], director: null, runtime: null, year: title.match(/\((\d{4})\)/)?.[1] || '' });
      }
    }

    schedule.push({ date, day, movies, reason: reason || '' });
  }

  console.log(JSON.stringify(schedule, null, 2));
}

main().catch(e => { console.error(e); process.exit(1); });
